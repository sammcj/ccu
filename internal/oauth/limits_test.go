package oauth

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// liveResponseSample mirrors the shape returned by
// GET https://api.anthropic.com/api/oauth/usage as of 2026-07-09, trimmed to
// the fields CCU reads. Note seven_day_sonnet/seven_day_opus are now null and
// the per-model weekly limit arrives via the `limits` array instead.
const liveResponseSample = `{
  "five_hour": {"utilization": 16, "resets_at": "2026-07-09T03:10:00.303205+00:00"},
  "seven_day": {"utilization": 49, "resets_at": "2026-07-10T04:00:00.303228+00:00"},
  "seven_day_sonnet": null,
  "seven_day_opus": null,
  "limits": [
    {"group": "session", "kind": "session", "percent": 16,
     "resets_at": "2026-07-09T03:10:00.303205+00:00", "scope": null,
     "severity": "normal", "is_active": false},
    {"group": "weekly", "kind": "weekly_all", "percent": 49,
     "resets_at": "2026-07-10T04:00:00.303228+00:00", "scope": null,
     "severity": "normal", "is_active": true},
    {"group": "weekly", "kind": "weekly_scoped", "percent": 45,
     "resets_at": "2026-07-10T04:00:00.303565+00:00",
     "scope": {"model": {"display_name": "Fable", "id": null}, "surface": null},
     "severity": "normal", "is_active": false}
  ]
}`

func TestUsageDataDecodesLimitsArray(t *testing.T) {
	var usage UsageData
	require.NoError(t, json.Unmarshal([]byte(liveResponseSample), &usage))

	require.Len(t, usage.Limits, 3)
	assert.Equal(t, KindSession, usage.Limits[0].Kind)
	assert.Equal(t, KindWeeklyAll, usage.Limits[1].Kind)
	assert.True(t, usage.Limits[1].IsActive)

	fable := usage.Limits[2]
	assert.Equal(t, KindWeeklyScoped, fable.Kind)
	assert.Equal(t, "Fable", fable.ModelName())
	assert.InDelta(t, 45.0, fable.Percent, 0.01)
	require.NotNil(t, fable.ResetsAt)
	assert.Nil(t, fable.Scope.Surface)
	assert.Nil(t, fable.Scope.Model.ID)
}

func TestLimitModelName(t *testing.T) {
	surface := "web"
	tests := []struct {
		name  string
		limit Limit
		want  string
	}{
		{name: "nil scope", limit: Limit{}, want: ""},
		{name: "scope without model", limit: Limit{Scope: &LimitScope{Surface: &surface}}, want: ""},
		{
			name:  "model scoped",
			limit: Limit{Scope: &LimitScope{Model: &LimitModel{DisplayName: "Fable"}}},
			want:  "Fable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.limit.ModelName())
		})
	}
}

func TestLimitKeyAndLabel(t *testing.T) {
	web := "web"
	tests := []struct {
		name      string
		limit     Limit
		wantKey   string
		wantLabel string
	}{
		{
			name:      "model only",
			limit:     Limit{Scope: &LimitScope{Model: &LimitModel{DisplayName: "Fable"}}},
			wantKey:   "fable",
			wantLabel: "Fable",
		},
		{
			name:      "model and surface",
			limit:     Limit{Scope: &LimitScope{Model: &LimitModel{DisplayName: "Fable"}, Surface: &web}},
			wantKey:   "fable/web",
			wantLabel: "Fable (web)",
		},
		{name: "unscoped", limit: Limit{}, wantKey: "", wantLabel: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantKey, tt.limit.Key())
			assert.Equal(t, tt.wantLabel, tt.limit.Label())
		})
	}
}

// The same model can carry one limit per surface; both must survive.
func TestWeeklyModelLimitsKeepsSurfaceScopedDuplicates(t *testing.T) {
	web, cli := "web", "cli"
	usage := UsageData{Limits: []Limit{
		{Kind: KindWeeklyScoped, Percent: 45, Scope: &LimitScope{Model: &LimitModel{DisplayName: "Fable"}, Surface: &web}},
		{Kind: KindWeeklyScoped, Percent: 12, Scope: &LimitScope{Model: &LimitModel{DisplayName: "Fable"}, Surface: &cli}},
	}}

	got := usage.WeeklyModelLimits()

	require.Len(t, got, 2)
	// Sorted by Key, so "fable/cli" precedes "fable/web"
	assert.Equal(t, "fable/cli", got[0].Key())
	assert.Equal(t, "fable/web", got[1].Key())
}

func TestWeeklyModelLimits(t *testing.T) {
	resetsAt := "2026-07-10T04:00:00Z"

	scopedLimit := func(name string, percent float64) Limit {
		return newScopedWeeklyLimit(name, percent, &resetsAt)
	}

	tests := []struct {
		name       string
		usage      UsageData
		wantModels []string
	}{
		{
			name:       "empty response yields no rows",
			usage:      UsageData{},
			wantModels: nil,
		},
		{
			name: "limits array wins and is sorted by model name",
			usage: UsageData{Limits: []Limit{
				scopedLimit("Opus", 10),
				scopedLimit("Fable", 45),
			}},
			wantModels: []string{"Fable", "Opus"},
		},
		{
			name: "session and weekly_all entries are excluded",
			usage: UsageData{Limits: []Limit{
				{Kind: KindSession, Percent: 16},
				{Kind: KindWeeklyAll, Percent: 49},
				scopedLimit("Fable", 45),
			}},
			wantModels: []string{"Fable"},
		},
		{
			name: "scoped entry without a model name is ignored",
			usage: UsageData{Limits: []Limit{
				{Kind: KindWeeklyScoped, Percent: 5},
				scopedLimit("Fable", 45),
			}},
			wantModels: []string{"Fable"},
		},
		{
			name: "falls back to legacy sonnet field",
			usage: UsageData{SevenDaySonnet: &struct {
				Utilisation float64 `json:"utilization"`
				ResetsAt    string  `json:"resets_at"`
			}{Utilisation: 25, ResetsAt: resetsAt}},
			wantModels: []string{"Sonnet"},
		},
		{
			name: "legacy opus without a reset time is not enforced",
			usage: UsageData{SevenDayOpus: &struct {
				Utilisation float64 `json:"utilization"`
				ResetsAt    *string `json:"resets_at"`
			}{Utilisation: 10, ResetsAt: nil}},
			wantModels: nil,
		},
		{
			name: "legacy opus with a reset time is enforced",
			usage: UsageData{SevenDayOpus: &struct {
				Utilisation float64 `json:"utilization"`
				ResetsAt    *string `json:"resets_at"`
			}{Utilisation: 10, ResetsAt: &resetsAt}},
			wantModels: []string{"Opus"},
		},
		{
			name: "limits array suppresses the legacy fallback",
			usage: UsageData{
				Limits: []Limit{scopedLimit("Fable", 45)},
				SevenDaySonnet: &struct {
					Utilisation float64 `json:"utilization"`
					ResetsAt    string  `json:"resets_at"`
				}{Utilisation: 25, ResetsAt: resetsAt},
			},
			wantModels: []string{"Fable"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.usage.WeeklyModelLimits()

			var names []string
			for _, l := range got {
				names = append(names, l.ModelName())
			}
			assert.Equal(t, tt.wantModels, names)
		})
	}
}

func TestWeeklyModelLimitsFromLiveSample(t *testing.T) {
	var usage UsageData
	require.NoError(t, json.Unmarshal([]byte(liveResponseSample), &usage))

	limits := usage.WeeklyModelLimits()
	require.Len(t, limits, 1)
	assert.Equal(t, "Fable", limits[0].ModelName())
	assert.InDelta(t, 45.0, limits[0].Percent, 0.01)
}

// is_active marks the limit currently binding the account, not whether a limit
// is enforced. In the live sample the 5-hour session and the enforced Fable
// weekly cap are both is_active=false while weekly_all (the highest bucket) is
// true. Filtering scoped limits on it would hide the Fable bar entirely.
func TestWeeklyModelLimitsIgnoresIsActive(t *testing.T) {
	var usage UsageData
	require.NoError(t, json.Unmarshal([]byte(liveResponseSample), &usage))

	for _, l := range usage.Limits {
		if l.Kind == KindWeeklyScoped {
			require.False(t, l.IsActive, "live sample's scoped limit is expected to be inactive")
		}
	}

	assert.Len(t, usage.WeeklyModelLimits(), 1, "an inactive scoped limit must still produce a bar")
}
