package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/sammcj/ccu/internal/models"
	"github.com/sammcj/ccu/internal/oauth"
	"github.com/stretchr/testify/assert"
)

// TestStaleUtilisationDetection tests that stale utilisation values are detected after session rollover
func TestStaleUtilisationDetection(t *testing.T) {
	tests := []struct {
		name                 string
		resetTimeOffset      time.Duration // Offset from "now"
		utilisation          float64
		expectStaleDetection bool
	}{
		{
			name:                 "session 30min old with 100% utilisation - stale",
			resetTimeOffset:      -30 * time.Minute,
			utilisation:          100.0,
			expectStaleDetection: true,
		},
		{
			name:                 "session 30min old with 5% utilisation - not stale",
			resetTimeOffset:      -30 * time.Minute,
			utilisation:          5.0,
			expectStaleDetection: false,
		},
		{
			name:                 "session 1hr old with 50% utilisation - stale (max ~20%)",
			resetTimeOffset:      -1 * time.Hour,
			utilisation:          50.0,
			expectStaleDetection: true,
		},
		{
			name:                 "session 1hr old with 15% utilisation - not stale",
			resetTimeOffset:      -1 * time.Hour,
			utilisation:          15.0,
			expectStaleDetection: false,
		},
		{
			name:                 "session 4hr old with 80% utilisation - not stale (max ~80%)",
			resetTimeOffset:      -4 * time.Hour,
			utilisation:          80.0,
			expectStaleDetection: false,
		},
		{
			name:                 "reset time in future - utilisation is current, not stale",
			resetTimeOffset:      1 * time.Hour,
			utilisation:          95.0,
			expectStaleDetection: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now()
			resetTime := now.Add(tt.resetTimeOffset)

			oauthData := &oauth.UsageData{}
			oauthData.FiveHour.ResetsAt = resetTime.Format(time.RFC3339Nano)
			oauthData.FiveHour.Utilisation = tt.utilisation

			// Simulate the stale detection logic from renderSessionMetricsFromOAuth
			sessionJustRolledOver := !resetTime.After(now)

			isStale := false
			if sessionJustRolledOver {
				elapsed := now.Sub(resetTime)
				maxReasonablePercent := (elapsed.Hours() / 5.0) * 100
				if maxReasonablePercent < 1 {
					maxReasonablePercent = 1
				}
				if tt.utilisation > maxReasonablePercent*2 {
					isStale = true
				}
			}

			assert.Equal(t, tt.expectStaleDetection, isStale,
				"Stale detection mismatch for %s", tt.name)
		})
	}
}

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

		// Simple names
		{"opus", "Opus"},
		{"sonnet", "Sonnet"},
		{"haiku", "Haiku"},

		// Unknown models pass through
		{"unknown-model", "unknown-model"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := formatModelNameSimple(tt.input)
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
	limits := models.Limits{CostLimitUSD: 100}

	result := renderPredictionWithOAuth(oauthData, session, 0, limits, now, false)

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
	limits := models.Limits{CostLimitUSD: 100}

	result := renderPredictionWithOAuth(oauthData, session, 0, limits, now, false)

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
	now := weekStart.Add(6 * 24 * time.Hour)           // Dec 6 10:00 UTC (6 days in)

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
	limits := models.Limits{CostLimitUSD: 100}

	result := renderPredictionWithOAuth(oauthData, session, 0, limits, now, true)

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
	limits := models.Limits{CostLimitUSD: 100}

	result := renderPredictionWithOAuth(oauthData, session, 0, limits, now, true)

	// Should contain ordinal day format (e.g. "8th" or "9th" depending on exact calculation)
	assert.Contains(t, result, "Weekly limit:", "should contain weekly limit label")
	// The depletion date should have an ordinal suffix
	assert.Regexp(t, `\d{1,2}(st|nd|rd|th)`, result, "should contain ordinal day suffix")
}
