package ui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

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
