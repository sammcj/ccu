package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sammcj/ccu/internal/analysis"
	"github.com/sammcj/ccu/internal/models"
	"github.com/sammcj/ccu/internal/oauth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatModelNameSimple(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Model names with dates
		{"claude-opus-4-5-20251101", "Opus 4.5"},
		{"claude-sonnet-4-20250514", "Sonnet 4"},
		{"claude-haiku-3-5-20241022", "Haiku 3.5"},
		{"claude-sonnet-5-1-20260301", "Sonnet 5.1"},
		{"claude-opus-5-20260101", "Opus 5"},

		// Without dates
		{"claude-opus-4-5", "Opus 4.5"},
		{"claude-sonnet-4", "Sonnet 4"},
		{"opus-4-5", "Opus 4.5"},
		{"sonnet", "Sonnet"},
		{"claude-3-opus", "Opus"},
		{"claude-3-5-sonnet", "Sonnet"},
		{"claude-3-haiku", "Haiku"},

		// Fable / Mythos
		{"claude-fable-5", "Fable 5"},
		{"claude-mythos-5", "Mythos 5"},

		// Simple names
		{"opus", "Opus"},
		{"sonnet", "Sonnet"},
		{"haiku", "Haiku"},

		// Unknown models pass through
		{"unknown-model", "unknown-model"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := FormatModelNameSimple(tt.input)
			assert.Equal(t, tt.expected, result, "Model name formatting mismatch")
		})
	}
}

// TestResetTimeCalculation tests that reset times in the past are correctly adjusted
func TestResetTimeCalculation(t *testing.T) {
	tests := []struct {
		name            string
		resetTimeStr    string
		currentTime     string
		expectedResetIn time.Duration
	}{
		{
			name:            "Reset time in past - should add 5 hours",
			resetTimeStr:    "2025-12-03T12:00:00Z",
			currentTime:     "2025-12-03T12:57:00Z",
			expectedResetIn: 4*time.Hour + 3*time.Minute, // 5:00 PM - 12:57 PM = 4h 3m
		},
		{
			name:            "Reset time in future - no adjustment needed",
			resetTimeStr:    "2025-12-03T17:00:00Z",
			currentTime:     "2025-12-03T12:57:00Z",
			expectedResetIn: 4*time.Hour + 3*time.Minute,
		},
		{
			name:            "Reset time exactly now - should add 5 hours",
			resetTimeStr:    "2025-12-03T13:00:00Z",
			currentTime:     "2025-12-03T13:00:00Z",
			expectedResetIn: 5 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetTime, err := time.Parse(time.RFC3339, tt.resetTimeStr)
			assert.NoError(t, err)

			now, err := time.Parse(time.RFC3339, tt.currentTime)
			assert.NoError(t, err)

			// Apply the same logic as renderSessionMetricsFromOAuth
			if !resetTime.After(now) {
				resetTime = resetTime.Add(5 * time.Hour)
			}

			timeUntilReset := resetTime.Sub(now)

			// Allow 1 second tolerance for time calculations
			assert.InDelta(t, tt.expectedResetIn.Seconds(), timeUntilReset.Seconds(), 1.0,
				"Reset time should be %v from now, got %v", tt.expectedResetIn, timeUntilReset)
		})
	}
}

func TestDayOrdinalSuffix(t *testing.T) {
	tests := []struct {
		day      int
		expected string
	}{
		{1, "st"}, {2, "nd"}, {3, "rd"}, {4, "th"},
		{11, "th"}, {12, "th"}, {13, "th"},
		{21, "st"}, {22, "nd"}, {23, "rd"}, {24, "th"},
		{31, "st"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("day_%d", tt.day), func(t *testing.T) {
			assert.Equal(t, tt.expected, dayOrdinalSuffix(tt.day))
		})
	}
}

func TestPredictionAfterSessionReset(t *testing.T) {
	// When session depletion is within 1 hour after reset, should show "(after reset)"
	now := time.Date(2025, 12, 3, 12, 0, 0, 0, time.UTC)
	resetTime := time.Date(2025, 12, 3, 14, 0, 0, 0, time.UTC) // 2 hours from now

	oauthData := &oauth.UsageData{
		FetchedAt: now,
	}
	oauthData.FiveHour.ResetsAt = resetTime.Format(time.RFC3339)
	oauthData.FiveHour.Utilisation = 60 // 60% used in 3 hours (session started at 9:00)
	oauthData.SevenDay.ResetsAt = time.Date(2025, 12, 7, 10, 0, 0, 0, time.UTC).Format(time.RFC3339)
	oauthData.SevenDay.Utilisation = 20

	session := &models.SessionBlock{IsActive: true}

	result := renderPredictionWithOAuth(oauthData, session, now, false)

	// Session start: 14:00 - 5h = 09:00. Elapsed: 3h = 180min.
	// Rate: 60%/180min = 0.333%/min. Remaining: 40%.
	// Minutes to depletion: 40/0.333 = 120min = 2h.
	// Depletion: 12:00 + 2h = 14:00, exactly at reset.
	// costDepletion.Before(resetTime) is false, and Sub(resetTime) = 0 <= 1h.
	assert.Contains(t, result, "after reset")
}

func TestPredictionNotShownWhenFarAfterReset(t *testing.T) {
	// When session depletion is more than 1 hour after reset, no session prediction shown
	now := time.Date(2025, 12, 3, 12, 0, 0, 0, time.UTC)
	resetTime := time.Date(2025, 12, 3, 13, 0, 0, 0, time.UTC) // 1 hour from now

	oauthData := &oauth.UsageData{
		FetchedAt: now,
	}
	oauthData.FiveHour.ResetsAt = resetTime.Format(time.RFC3339)
	oauthData.FiveHour.Utilisation = 10 // Low usage, depletion far away
	oauthData.SevenDay.ResetsAt = time.Date(2025, 12, 7, 10, 0, 0, 0, time.UTC).Format(time.RFC3339)
	oauthData.SevenDay.Utilisation = 5

	session := &models.SessionBlock{IsActive: true}

	result := renderPredictionWithOAuth(oauthData, session, now, false)

	// Low usage rate = depletion far after reset. Should not show session prediction or "after reset".
	assert.NotContains(t, result, "Session limit:")
	assert.NotContains(t, result, "after reset")
}

func TestWeeklyPredictionAfterReset(t *testing.T) {
	// Weekly depletion within 1 day after weekly reset should show "(after reset)"
	// Set now to 6 days into the week with ~85% utilisation so the linear projection
	// lands just after the weekly reset.
	weeklyReset := time.Date(2025, 12, 7, 10, 0, 0, 0, time.UTC)
	weekStart := weeklyReset.Add(-7 * 24 * time.Hour) // Nov 30 10:00 UTC
	now := weekStart.Add(6 * 24 * time.Hour)          // Dec 6 10:00 UTC (6 days in)

	// 85% used in 6 days = ~14.17%/day. Remaining 15% takes ~1.06 days.
	// Depletion: Dec 6 10:00 + ~25.4h = ~Dec 7 11:24 (after Dec 7 10:00 reset).
	// That's ~1.4h after reset, which is <= 24h.
	oauthData := &oauth.UsageData{
		FetchedAt: now,
	}
	sessionReset := now.Add(3 * time.Hour) // Session resets in 3h (doesn't matter for weekly)
	oauthData.FiveHour.ResetsAt = sessionReset.Format(time.RFC3339)
	oauthData.FiveHour.Utilisation = 30
	oauthData.SevenDay.ResetsAt = weeklyReset.Format(time.RFC3339)
	oauthData.SevenDay.Utilisation = 85

	session := &models.SessionBlock{IsActive: true}

	result := renderPredictionWithOAuth(oauthData, session, now, true)

	assert.Contains(t, result, "after reset", "should show weekly near-miss after reset")
	assert.Contains(t, result, "Weekly limit:", "should contain weekly limit label")
}

func TestWeeklyPredictionDateFormat(t *testing.T) {
	// Verify weekly prediction uses "Mon 27th" format with ordinal day
	weeklyReset := time.Date(2025, 12, 10, 10, 0, 0, 0, time.UTC)
	weekStart := weeklyReset.Add(-7 * 24 * time.Hour)
	now := weekStart.Add(5 * 24 * time.Hour) // 5 days in

	// 90% used in 5 days = depletion well before reset
	oauthData := &oauth.UsageData{
		FetchedAt: now,
	}
	sessionReset := now.Add(3 * time.Hour)
	oauthData.FiveHour.ResetsAt = sessionReset.Format(time.RFC3339)
	oauthData.FiveHour.Utilisation = 20
	oauthData.SevenDay.ResetsAt = weeklyReset.Format(time.RFC3339)
	oauthData.SevenDay.Utilisation = 90

	session := &models.SessionBlock{IsActive: true}

	result := renderPredictionWithOAuth(oauthData, session, now, true)

	// Should contain ordinal day format (e.g. "8th" or "9th" depending on exact calculation)
	assert.Contains(t, result, "Weekly limit:", "should contain weekly limit label")
	// The depletion date should have an ordinal suffix
	assert.Regexp(t, `\d{1,2}(st|nd|rd|th)`, result, "should contain ordinal day suffix")
}

func TestGetSessionDistributionString(t *testing.T) {
	base := time.Date(2025, 12, 3, 12, 30, 0, 0, time.UTC)

	entries := []models.UsageEntry{
		{Timestamp: base, Model: "claude-opus-4-5-20251101", CostUSD: 2.0, InputTokens: 100, OutputTokens: 50},
		{Timestamp: base.Add(5 * time.Minute), Model: "claude-sonnet-4-20250514", CostUSD: 0.5, InputTokens: 100, OutputTokens: 50},
		{Timestamp: base.Add(10 * time.Minute), Model: "claude-sonnet-4", CostUSD: 0.5, InputTokens: 100, OutputTokens: 50},
	}
	blocks := analysis.CreateSessionBlocks(entries)
	assert.Len(t, blocks, 1)

	got := getSessionDistributionString(&blocks[0])

	// Cost-ranked descending with normalised, formatted names; both sonnet raw
	// names merge into one share via NormaliseModelName.
	expected := "[" +
		GetModelStyle("claude-opus-4-5").Render("Opus 4.5") + ": 66.7%, " +
		GetModelStyle("claude-sonnet-4").Render("Sonnet 4") + ": 33.3%]"
	assert.Equal(t, expected, got)
}

func TestGetSessionDistributionStringEmpty(t *testing.T) {
	assert.Empty(t, getSessionDistributionString(nil))
	assert.Empty(t, getSessionDistributionString(&models.SessionBlock{}))
}

func TestRenderSessionCacheHitRate(t *testing.T) {
	base := time.Date(2025, 12, 3, 12, 30, 0, 0, time.UTC)
	const barWidth = 45

	entries := []models.UsageEntry{
		{Timestamp: base, Model: "claude-opus-4-5", InputTokens: 60, CacheCreationTokens: 200, CacheReadTokens: 400, CostUSD: 1.0},
		{Timestamp: base.Add(5 * time.Minute), Model: "claude-sonnet-4", InputTokens: 40, CacheCreationTokens: 100, CacheReadTokens: 200, CostUSD: 0.5},
	}
	blocks := analysis.CreateSessionBlocks(entries)
	assert.Len(t, blocks, 1)

	got := renderSessionCacheHitRate(&blocks[0], barWidth)

	// Token classes summed by hand from the entries above: the per-model
	// aggregates must produce the same rendered line the entry loop did.
	rate := analysis.CalculateCacheHitRate(100, 300, 600)
	expected := renderCacheHitRateLine("Session - Cache Hit:", rate, barWidth)
	assert.Equal(t, expected, got)
	assert.Contains(t, got, "60.0%")
}

func TestRenderSessionCacheHitRateNoActivity(t *testing.T) {
	// Width is irrelevant with no activity; a narrower bar also exercises the parameter
	const barWidth = 30
	assert.Empty(t, renderSessionCacheHitRate(nil, barWidth))
	assert.Empty(t, renderSessionCacheHitRate(&models.SessionBlock{IsGap: true}, barWidth))
	assert.Empty(t, renderSessionCacheHitRate(&models.SessionBlock{}, barWidth))
}

func TestWeeklySectionShownWithoutPerModelFields(t *testing.T) {
	// The API always returns the combined seven_day field but only includes
	// seven_day_sonnet/seven_day_opus above a usage threshold. The weekly
	// section must still render from the combined field alone.
	now := time.Now().UTC()

	oauthData := &oauth.UsageData{FetchedAt: now}
	oauthData.FiveHour.ResetsAt = now.Add(2 * time.Hour).Format(time.RFC3339)
	oauthData.FiveHour.Utilisation = 10
	oauthData.SevenDay.ResetsAt = now.Add(4 * 24 * time.Hour).Format(time.RFC3339)
	oauthData.SevenDay.Utilisation = 5

	result := RenderDashboard(DashboardData{
		Config:         models.DefaultConfig(),
		CurrentSession: &models.SessionBlock{IsActive: true, StartTime: now.Add(-time.Hour), EndTime: now.Add(4 * time.Hour)},
		OAuthData:      oauthData,
	})

	assert.Contains(t, result, "Weekly - All Models:")
	assert.NotContains(t, result, "Weekly - Sonnet:")
	assert.NotContains(t, result, "Weekly - Opus:")
}

func TestRenderWeeklyUsageFromOAuth_ScopedLimits(t *testing.T) {
	now := time.Now().UTC()
	resetsAt := now.Add(4 * 24 * time.Hour).Format(time.RFC3339)

	oauthData := &oauth.UsageData{FetchedAt: now}
	oauthData.SevenDay.ResetsAt = resetsAt
	oauthData.SevenDay.Utilisation = 49
	oauthData.Limits = []oauth.Limit{
		{Kind: oauth.KindWeeklyAll, Percent: 49},
		{
			Kind:     oauth.KindWeeklyScoped,
			Percent:  45,
			ResetsAt: &resetsAt,
			Scope:    &oauth.LimitScope{Model: &oauth.LimitModel{DisplayName: "Fable"}},
		},
		{
			Kind:     oauth.KindWeeklyScoped,
			Percent:  25,
			ResetsAt: &resetsAt,
			Scope:    &oauth.LimitScope{Model: &oauth.LimitModel{DisplayName: "Sonnet"}},
		},
	}

	limits := models.Limits{PlanName: "max5"}
	lines := renderWeeklyUsageFromOAuth(oauthData, limits, 45)

	require.Len(t, lines, 3)
	assert.Contains(t, lines[0], "Weekly - All Models:")

	// Sorted by model name, so Fable precedes Sonnet
	assert.Contains(t, lines[1], "Fable")
	assert.Contains(t, lines[1], "45.0%")
	// Fable has no published hour allowance, and its reset matches All Models,
	// so the trailing column is dropped rather than repeating the same time
	assert.NotContains(t, lines[1], "[Resets:")
	assert.NotContains(t, lines[1], "hrs")

	assert.Contains(t, lines[2], "Sonnet")
	assert.Contains(t, lines[2], "(52.5 / 210.0 hrs)")
}

// A model that resets on its own schedule still shows its reset time.
func TestRenderWeeklyUsageFromOAuth_DistinctResetTimeIsShown(t *testing.T) {
	now := time.Now().UTC()
	allReset := now.Add(4 * 24 * time.Hour).Format(time.RFC3339)
	fableReset := now.Add(6 * 24 * time.Hour).Format(time.RFC3339)

	oauthData := &oauth.UsageData{FetchedAt: now}
	oauthData.SevenDay.ResetsAt = allReset
	oauthData.SevenDay.Utilisation = 49
	oauthData.Limits = []oauth.Limit{{
		Kind:     oauth.KindWeeklyScoped,
		Percent:  45,
		ResetsAt: &fableReset,
		Scope:    &oauth.LimitScope{Model: &oauth.LimitModel{DisplayName: "Fable"}},
	}}

	lines := renderWeeklyUsageFromOAuth(oauthData, models.Limits{PlanName: "max5"}, 45)

	require.Len(t, lines, 2)
	assert.Contains(t, lines[1], "[Resets:")
}

// Sub-second differences in resets_at must not defeat the redundancy check,
// since the rendered time only has minute granularity.
func TestRenderWeeklyUsageFromOAuth_SubSecondResetDriftIsTreatedAsSame(t *testing.T) {
	now := time.Now().UTC().Add(4 * 24 * time.Hour).Truncate(time.Hour)
	allReset := now.Format("2006-01-02T15:04:05.000000Z07:00")
	fableReset := now.Add(337 * time.Microsecond).Format("2006-01-02T15:04:05.000000Z07:00")
	require.NotEqual(t, allReset, fableReset)

	oauthData := &oauth.UsageData{FetchedAt: time.Now().UTC()}
	oauthData.SevenDay.ResetsAt = allReset
	oauthData.Limits = []oauth.Limit{{
		Kind:     oauth.KindWeeklyScoped,
		Percent:  45,
		ResetsAt: &fableReset,
		Scope:    &oauth.LimitScope{Model: &oauth.LimitModel{DisplayName: "Fable"}},
	}}

	lines := renderWeeklyUsageFromOAuth(oauthData, models.Limits{PlanName: "max5"}, 45)

	require.Len(t, lines, 2)
	assert.NotContains(t, lines[1], "[Resets:")
}

func TestRenderWeeklyUsageFromOAuth_UnknownModelStillRenders(t *testing.T) {
	now := time.Now().UTC()
	resetsAt := now.Add(4 * 24 * time.Hour).Format(time.RFC3339)

	oauthData := &oauth.UsageData{FetchedAt: now}
	oauthData.SevenDay.ResetsAt = resetsAt
	oauthData.Limits = []oauth.Limit{{
		Kind:     oauth.KindWeeklyScoped,
		Percent:  12,
		ResetsAt: &resetsAt,
		Scope:    &oauth.LimitScope{Model: &oauth.LimitModel{DisplayName: "Nimbus"}},
	}}

	const barWidth = 30
	lines := renderWeeklyUsageFromOAuth(oauthData, models.Limits{PlanName: "max5"}, barWidth)

	require.Len(t, lines, 2)
	assert.Contains(t, lines[1], "Nimbus")
	assert.Contains(t, lines[1], "12.0%")

	// The bar honours the requested width: barWidth-2 cells between the brackets
	cells := strings.Count(lines[1], "█") + strings.Count(lines[1], "░")
	assert.Equal(t, barWidth-2, cells)
}

func TestRenderWeeklyCacheHitRate(t *testing.T) {
	now := time.Date(2025, 12, 3, 12, 0, 0, 0, time.UTC)
	const barWidth = 45

	entries := []models.UsageEntry{
		// Older than 7 days - excluded (its cache reads would drag the rate up)
		{Timestamp: now.Add(-8 * 24 * time.Hour), Model: "claude-sonnet-4", InputTokens: 10, CacheReadTokens: 10000, CostUSD: 0.1},
		// Within the window
		{Timestamp: now.Add(-2 * time.Hour), Model: "claude-opus-4-5", InputTokens: 150, CacheCreationTokens: 250, CacheReadTokens: 300, CostUSD: 1.0},
		{Timestamp: now.Add(-1 * time.Hour), Model: "claude-sonnet-4", InputTokens: 50, CacheCreationTokens: 50, CacheReadTokens: 200, CostUSD: 0.5},
	}
	blocks := analysis.CreateSessionBlocks(entries)

	got := renderWeeklyCacheHitRate(blocks, now, barWidth)

	// Only the recent block counts: 500 cache reads / 1000 total = 50%.
	rate := analysis.CalculateCacheHitRate(200, 300, 500)
	expected := renderCacheHitRateLine("Weekly - Cache Hit:", rate, barWidth)
	assert.Equal(t, expected, got)
	assert.Contains(t, got, "50.0%")
}

func TestRenderWeeklyCacheHitRateHiddenWhenHealthy(t *testing.T) {
	now := time.Date(2025, 12, 3, 12, 0, 0, 0, time.UTC)
	entries := []models.UsageEntry{
		{Timestamp: now.Add(-1 * time.Hour), Model: "claude-sonnet-4", InputTokens: 50, CacheReadTokens: 950, CostUSD: 0.5},
	}
	blocks := analysis.CreateSessionBlocks(entries)
	assert.Empty(t, renderWeeklyCacheHitRate(blocks, now, 45))
}
