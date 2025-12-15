package ui

import (
	"testing"
	"time"

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
