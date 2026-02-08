package analysis

import (
	"testing"
	"time"

	"github.com/sammcj/ccu/internal/models"
	"github.com/sammcj/ccu/internal/oauth"
	"github.com/stretchr/testify/assert"
)

func TestCalculateWeeklyUsage(t *testing.T) {
	now := time.Now()

	entries := []models.UsageEntry{
		// Session 1 (Sonnet) - from 8 hours ago
		{Timestamp: now.Add(-8 * time.Hour), InputTokens: 100, OutputTokens: 50, Model: "claude-sonnet-4-5"},    // 150 tokens
		{Timestamp: now.Add(-7 * time.Hour), InputTokens: 200, OutputTokens: 100, Model: "claude-sonnet-4-5"},   // 300 tokens
		// Session 1 total: 450 tokens (Sonnet)

		// Session 2 (Opus) - from 5.5 hours ago (gap > 1 hour from session 1 end)
		{Timestamp: now.Add(-5*time.Hour - 30*time.Minute), InputTokens: 80, OutputTokens: 20, Model: "claude-opus-4-5"}, // 100 tokens
		{Timestamp: now.Add(-5 * time.Hour), InputTokens: 50, OutputTokens: 25, Model: "claude-opus-4-5"},                 // 75 tokens
		// Session 2 total: 175 tokens (Opus)

		// Session 3 (Mixed) - from 2 hours ago (well within previous session's end + gap threshold)
		{Timestamp: now.Add(-2 * time.Hour), InputTokens: 150, OutputTokens: 100, Model: "claude-sonnet-4-5"}, // 250 tokens
		{Timestamp: now.Add(-1 * time.Hour), InputTokens: 60, OutputTokens: 40, Model: "claude-haiku-4-5"},    // 100 tokens
		// Session 3 total: 350 tokens (250 Sonnet + 100 Haiku)
	}

	// Expected totals:
	// Total tokens: 450 + 175 + 350 = 975
	// Sonnet tokens: 450 + 250 = 700
	// Opus tokens: 175
	// Haiku tokens: 100 (but we only track Sonnet/Opus in weekly usage)

	// Create session blocks from entries
	blocks := CreateSessionBlocks(entries)

	result := CalculateWeeklyUsage(blocks, "max5", now)

	assert.Equal(t, 975, result.TotalTokens, "Total tokens should be sum of all tokens from all session blocks")
	assert.Equal(t, 700, result.SonnetTokens, "Sonnet tokens should be sum from all sonnet entries across all sessions")
	assert.Equal(t, 175, result.OpusTokens, "Opus tokens should be sum from all opus entries across all sessions")

	// Verify hour calculations using plan-specific rates
	// For Max5: 88,000 tokens / 5 hours * 0.6 efficiency = 10,560 tokens/hour for Sonnet
	// For Opus: 10,560 * 0.5 = 5,280 tokens/hour
	sonnetRate, opusRate := models.GetTokensPerHour("max5")
	expectedSonnetHours := float64(700) / sonnetRate
	expectedOpusHours := float64(175) / opusRate

	assert.InDelta(t, expectedSonnetHours, result.SonnetHours, 0.001, "Sonnet hours should be correctly calculated")
	assert.InDelta(t, expectedOpusHours, result.OpusHours, 0.001, "Opus hours should be correctly calculated")
}

func TestPredictWeeklyDepletion(t *testing.T) {
	now := time.Date(2026, 2, 11, 21, 0, 0, 0, time.UTC) // Wednesday 9pm
	// Reset is next Sunday noon = Feb 15 12:00
	resetTime := time.Date(2026, 2, 15, 12, 0, 0, 0, time.UTC)
	resetStr := resetTime.Format(time.RFC3339Nano)

	makeOAuth := func(utilisation float64) *oauth.UsageData {
		d := &oauth.UsageData{}
		d.SevenDay.Utilisation = utilisation
		d.SevenDay.ResetsAt = resetStr
		return d
	}

	t.Run("no prediction when less than 24 hours elapsed", func(t *testing.T) {
		// Only 9 hours into the weekly window (weekStart = resetTime - 7d = Feb 8 12:00)
		earlyNow := time.Date(2026, 2, 8, 21, 0, 0, 0, time.UTC) // same day, 9 hours in
		result := PredictWeeklyDepletion(makeOAuth(11.0), 0, 0, earlyNow)

		assert.False(t, result.WillHitLimit, "should not predict limit from <24h of data")
		assert.True(t, result.DepletionTime.IsZero(), "depletion time should be zero")
	})

	t.Run("predicts depletion after 24 hours elapsed", func(t *testing.T) {
		// 3.375 days into the window, at 50% usage => will hit 100% in another 3.375 days
		// That's 6.75 days total, still before 7-day reset
		result := PredictWeeklyDepletion(makeOAuth(50.0), 0, 0, now)

		assert.True(t, result.WillHitLimit, "should predict hitting limit")
		assert.False(t, result.DepletionTime.IsZero(), "should have depletion time")
		assert.True(t, result.DepletionTime.Before(resetTime), "depletion should be before reset")
	})

	t.Run("no warning when usage rate is safe", func(t *testing.T) {
		// 3.375 days in at only 10% => would hit 100% in ~30 more days, well after reset
		result := PredictWeeklyDepletion(makeOAuth(10.0), 0, 0, now)

		assert.False(t, result.WillHitLimit, "should not predict hitting limit at low usage")
	})

	t.Run("already at limit", func(t *testing.T) {
		result := PredictWeeklyDepletion(makeOAuth(100.0), 0, 0, now)

		assert.True(t, result.WillHitLimit)
		assert.Equal(t, now, result.DepletionTime, "depletion should be now")
	})

	t.Run("over limit", func(t *testing.T) {
		result := PredictWeeklyDepletion(makeOAuth(120.0), 0, 0, now)

		assert.True(t, result.WillHitLimit)
		assert.Equal(t, now, result.DepletionTime)
	})
}
