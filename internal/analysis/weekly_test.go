package analysis

import (
	"testing"
	"time"

	"github.com/sammcj/ccu/internal/oauth"
	"github.com/stretchr/testify/assert"
)

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
