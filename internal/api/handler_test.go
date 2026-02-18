package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sammcj/ccu/internal/models"
	"github.com/sammcj/ccu/internal/oauth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockState implements StateProvider for testing.
type mockState struct {
	oauthData      *oauth.UsageData
	currentSession *models.SessionBlock
	sessions       []models.SessionBlock
	limits         models.Limits
	config         *models.Config
	lastRefresh    time.Time
	hasData        bool
}

func (m *mockState) GetOAuthData() *oauth.UsageData         { return m.oauthData }
func (m *mockState) GetCurrentSession() *models.SessionBlock { return m.currentSession }
func (m *mockState) GetSessions() []models.SessionBlock      { return m.sessions }
func (m *mockState) GetLimits() models.Limits                { return m.limits }
func (m *mockState) GetConfig() *models.Config               { return m.config }
func (m *mockState) GetLastRefresh() time.Time               { return m.lastRefresh }
func (m *mockState) HasData() bool                           { return m.hasData }

// baseTime is the fixed reference time used across all tests.
var baseTime = time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)

func newTestConfig() *models.Config {
	cfg := models.DefaultConfig()
	cfg.Plan = "max5"
	return cfg
}

func newTestLimits() models.Limits {
	return models.Limits{
		PlanName:     "Max5",
		CostLimitUSD: 35.0,
		MessageLimit: 1000,
	}
}

func newTestSession(now time.Time) *models.SessionBlock {
	start := now.Add(-2 * time.Hour)
	end := start.Add(5 * time.Hour)
	s := &models.SessionBlock{
		ID:           "test-session",
		StartTime:    start,
		EndTime:      end,
		TotalTokens:  150000,
		DisplayTokens: 100000,
		CostUSD:      5.0,
		IsActive:     true,
		CostBurnRate: 0.05, // USD/min
		MessageCount: 42,
		PerModelStats: map[string]*models.ModelStats{
			"claude-sonnet-4": {
				InputTokens:  60000,
				OutputTokens: 30000,
				CostUSD:      3.0,
				MessageCount: 30,
			},
			"claude-opus-4": {
				InputTokens:  8000,
				OutputTokens: 2000,
				CostUSD:      2.0,
				MessageCount: 12,
			},
		},
	}
	return s
}

func newTestOAuthData(now time.Time) *oauth.UsageData {
	resetsAt := now.Add(4 * 24 * time.Hour).Format(time.RFC3339Nano)
	return &oauth.UsageData{
		FiveHour: struct {
			Utilisation float64 `json:"utilization"`
			ResetsAt    string  `json:"resets_at"`
		}{
			Utilisation: 45.0,
			ResetsAt:    now.Add(3 * time.Hour).Format(time.RFC3339Nano),
		},
		SevenDay: struct {
			Utilisation float64 `json:"utilization"`
			ResetsAt    string  `json:"resets_at"`
		}{
			Utilisation: 30.0,
			ResetsAt:    resetsAt,
		},
		SevenDaySonnet: &struct {
			Utilisation float64 `json:"utilization"`
			ResetsAt    string  `json:"resets_at"`
		}{
			Utilisation: 25.0,
			ResetsAt:    resetsAt,
		},
		FetchedAt: now,
	}
}

func TestBuildStatusResponse_FullData(t *testing.T) {
	now := baseTime
	session := newTestSession(now)
	oauthData := newTestOAuthData(now)

	state := &mockState{
		oauthData:      oauthData,
		currentSession: session,
		sessions:       []models.SessionBlock{*session},
		limits:         newTestLimits(),
		config:         newTestConfig(),
		lastRefresh:    now.Add(-30 * time.Second),
		hasData:        true,
	}

	raw, err := BuildStatusResponse(state, now)
	require.NoError(t, err)
	require.NotEmpty(t, raw)

	var resp StatusResponse
	require.NoError(t, json.Unmarshal(raw, &resp))

	assert.Equal(t, "max5", resp.Plan)
	assert.Equal(t, now.UTC().Format(time.RFC3339), resp.ServerTime)
	assert.InDelta(t, 30, resp.DataAgeSeconds, 2)

	// Weekly section
	require.NotNil(t, resp.Weekly)
	require.NotNil(t, resp.Weekly.AllModels)
	assert.InDelta(t, 30.0, resp.Weekly.AllModels.UtilisationPct, 0.01)
	assert.Greater(t, resp.Weekly.AllModels.ResetsInSeconds, int64(0))

	require.NotNil(t, resp.Weekly.Sonnet)
	assert.InDelta(t, 25.0, resp.Weekly.Sonnet.UtilisationPct, 0.01)
	assert.Equal(t, float64(210), resp.Weekly.Sonnet.LimitHours)
	assert.InDelta(t, 210*0.25, resp.Weekly.Sonnet.UsedHours, 0.1)

	// Opus nil (no ResetsAt in test data)
	assert.Nil(t, resp.Weekly.Opus)

	// Session section – utilisation comes from OAuth FiveHour.Utilisation (45%), not local cost
	require.NotNil(t, resp.Session)
	assert.InDelta(t, 45.0, resp.Session.UtilisationPct, 0.01)
	assert.Equal(t, 42, resp.Session.MessageCount)
	assert.Greater(t, resp.Session.ElapsedSeconds, int64(0))
	assert.Greater(t, resp.Session.TotalSeconds, int64(0))
	assert.InDelta(t, 5.0, resp.Session.CostUSD, 0.01)

	// Model distribution sorted by cost desc
	require.Len(t, resp.Session.ModelDistribution, 2)
	assert.Equal(t, "claude-sonnet-4", resp.Session.ModelDistribution[0].Model)
	assert.Equal(t, "claude-opus-4", resp.Session.ModelDistribution[1].Model)
	assert.InDelta(t, 60.0, resp.Session.ModelDistribution[0].CostPct, 0.01)
	assert.InDelta(t, 40.0, resp.Session.ModelDistribution[1].CostPct, 0.01)

	// Burn rate section
	require.NotNil(t, resp.BurnRate)
	assert.InDelta(t, 0.05, resp.BurnRate.CostPerMinUSD, 0.001)
	assert.InDelta(t, 3.0, resp.BurnRate.CostPerHourUSD, 0.001)

	// Prediction section
	require.NotNil(t, resp.Prediction)
	// costRemaining = 35 - 5 = 30 USD, burnRate = 0.05 USD/min → 600 min → 10 h to depletion
	// Session ends in 3 h (now + 3h), so depletion is AFTER session end → WillHitLimit = false
	require.NotNil(t, resp.Prediction.SessionLimitAt)
	assert.False(t, resp.Prediction.SessionWillHitLimit)
}

func TestBuildStatusResponse_NoOAuth(t *testing.T) {
	now := baseTime
	session := newTestSession(now)

	state := &mockState{
		oauthData:      nil, // no OAuth
		currentSession: session,
		sessions:       []models.SessionBlock{*session},
		limits:         newTestLimits(),
		config:         newTestConfig(),
		lastRefresh:    now,
		hasData:        true,
	}

	raw, err := BuildStatusResponse(state, now)
	require.NoError(t, err)

	var resp StatusResponse
	require.NoError(t, json.Unmarshal(raw, &resp))

	// Weekly section should be nil when no OAuth data
	assert.Nil(t, resp.Weekly)

	// Session section should still be populated from session blocks
	require.NotNil(t, resp.Session)
	assert.Equal(t, 42, resp.Session.MessageCount)

	// Burn rate populated from session
	require.NotNil(t, resp.BurnRate)
}

func TestBuildStatusResponse_NoData(t *testing.T) {
	now := baseTime

	state := &mockState{
		oauthData:      nil,
		currentSession: nil,
		sessions:       nil,
		limits:         newTestLimits(),
		config:         newTestConfig(),
		lastRefresh:    now,
		hasData:        false,
	}

	raw, err := BuildStatusResponse(state, now)
	require.NoError(t, err)

	var resp StatusResponse
	require.NoError(t, json.Unmarshal(raw, &resp))

	assert.Equal(t, "max5", resp.Plan)
	assert.Nil(t, resp.Weekly)
	assert.Nil(t, resp.Session)
	require.NotNil(t, resp.BurnRate)
	assert.Equal(t, 0.0, resp.BurnRate.TokensPerMin)
}

func TestBuildStatusResponse_SizeUnder4KB(t *testing.T) {
	now := baseTime
	session := newTestSession(now)
	oauthData := newTestOAuthData(now)

	state := &mockState{
		oauthData:      oauthData,
		currentSession: session,
		sessions:       []models.SessionBlock{*session},
		limits:         newTestLimits(),
		config:         newTestConfig(),
		lastRefresh:    now,
		hasData:        true,
	}

	raw, err := BuildStatusResponse(state, now)
	require.NoError(t, err)
	assert.Less(t, len(raw), 4096, "response must be under 4 KiB for ESP32 compatibility")
}

func TestBuildStatusResponse_WeeklyPredictionPresentWhenWillHitLimitFalse(t *testing.T) {
	now := baseTime
	session := newTestSession(now)

	// Weekly utilisation low enough that WillHitLimit will be false,
	// but enough history (>24h elapsed) that a DepletionTime is computed.
	weekStart := now.Add(-48 * time.Hour) // 48h into the week
	resetsAt := weekStart.Add(7 * 24 * time.Hour).Format(time.RFC3339Nano)
	oauthData := &oauth.UsageData{
		FiveHour: struct {
			Utilisation float64 `json:"utilization"`
			ResetsAt    string  `json:"resets_at"`
		}{
			Utilisation: 10.0,
			ResetsAt:    now.Add(3 * time.Hour).Format(time.RFC3339Nano),
		},
		SevenDay: struct {
			Utilisation float64 `json:"utilization"`
			ResetsAt    string  `json:"resets_at"`
		}{
			Utilisation: 5.0, // low: won't hit limit before reset
			ResetsAt:    resetsAt,
		},
		FetchedAt: now,
	}

	state := &mockState{
		oauthData:      oauthData,
		currentSession: session,
		sessions:       []models.SessionBlock{*session},
		limits:         newTestLimits(),
		config:         newTestConfig(),
		lastRefresh:    now,
		hasData:        true,
	}

	raw, err := BuildStatusResponse(state, now)
	require.NoError(t, err)

	var resp StatusResponse
	require.NoError(t, json.Unmarshal(raw, &resp))

	require.NotNil(t, resp.Prediction)
	assert.False(t, resp.Prediction.WeeklyWillHitLimit)
	// Fields must still be present even though WillHitLimit is false
	assert.NotNil(t, resp.Prediction.WeeklyLimitAt, "weekly_limit_at must be present when prediction is computable")
	assert.NotNil(t, resp.Prediction.WeeklyLimitInSeconds, "weekly_limit_in_seconds must be present when prediction is computable")
}

func TestBuildStatusResponse_SessionStalenessClamp(t *testing.T) {
	now := baseTime

	// Session rolled over 10 minutes ago: the old session's CostUSD is still
	// cached, producing a high utilisation_pct that should be clamped to 0.
	rolloverTime := now.Add(-10 * time.Minute)
	sessionStart := rolloverTime // new session started at old reset time
	sessionEnd := sessionStart.Add(5 * time.Hour)

	session := &models.SessionBlock{
		ID:           "stale-session",
		StartTime:    sessionStart,
		EndTime:      sessionEnd,
		CostUSD:      30.0, // high – leftover from old session
		IsActive:     true,
		CostBurnRate: 0.05,
		MessageCount: 5,
	}

	oauthData := &oauth.UsageData{
		FiveHour: struct {
			Utilisation float64 `json:"utilization"`
			ResetsAt    string  `json:"resets_at"`
		}{
			// ResetsAt is 10 minutes in the past → session rolled over
			Utilisation: 85.0,
			ResetsAt:    rolloverTime.Format(time.RFC3339Nano),
		},
		SevenDay: struct {
			Utilisation float64 `json:"utilization"`
			ResetsAt    string  `json:"resets_at"`
		}{
			Utilisation: 20.0,
			ResetsAt:    now.Add(4 * 24 * time.Hour).Format(time.RFC3339Nano),
		},
		FetchedAt: now,
	}

	limits := models.Limits{PlanName: "Max5", CostLimitUSD: 35.0}
	state := &mockState{
		oauthData:      oauthData,
		currentSession: session,
		sessions:       []models.SessionBlock{*session},
		limits:         limits,
		config:         newTestConfig(),
		lastRefresh:    now,
		hasData:        true,
	}

	raw, err := BuildStatusResponse(state, now)
	require.NoError(t, err)

	var resp StatusResponse
	require.NoError(t, json.Unmarshal(raw, &resp))

	require.NotNil(t, resp.Session)
	// 10 minutes into a 5-hour session → maxReasonable = (10/60/5)*100 ≈ 3.3%
	// Raw utilisation 85.7% (30/35*100) >> 3.3%*2 → must be clamped to 0
	assert.Equal(t, 0.0, resp.Session.UtilisationPct, "stale utilisation must be clamped to 0")
}

func TestBuildStatusResponse_SessionStalenessNotAppliedWhenFresh(t *testing.T) {
	now := baseTime
	session := newTestSession(now) // session started 2h ago, ends in 3h
	oauthData := newTestOAuthData(now)
	// FiveHour.ResetsAt is now+3h → session has NOT rolled over

	state := &mockState{
		oauthData:      oauthData,
		currentSession: session,
		sessions:       []models.SessionBlock{*session},
		limits:         newTestLimits(),
		config:         newTestConfig(),
		lastRefresh:    now,
		hasData:        true,
	}

	raw, err := BuildStatusResponse(state, now)
	require.NoError(t, err)

	var resp StatusResponse
	require.NoError(t, json.Unmarshal(raw, &resp))

	require.NotNil(t, resp.Session)
	// Utilisation comes from OAuth FiveHour.Utilisation (45%) since OAuth data is present
	assert.InDelta(t, 45.0, resp.Session.UtilisationPct, 0.01)
}

func TestModelDisplayName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"claude-opus-4-20250514", "claude-opus-4"},
		{"claude-sonnet-4-20250514", "claude-sonnet-4"},
		{"claude-3-5-sonnet-20241022", "claude-3-5-sonnet"},
		{"claude-3-5-haiku-20241022", "claude-3-5-haiku"},
		{"claude-opus-4", "claude-opus-4"},
		{"unknown-model", "unknown-model"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, models.NormaliseModelName(tt.input))
		})
	}
}
