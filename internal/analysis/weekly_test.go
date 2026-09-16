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
		result := PredictWeeklyDepletion(makeOAuth(11.0), earlyNow)

		assert.False(t, result.WillHitLimit, "should not predict limit from <24h of data")
		assert.True(t, result.DepletionTime.IsZero(), "depletion time should be zero")
	})

	t.Run("predicts depletion after 24 hours elapsed", func(t *testing.T) {
		// 3.375 days into the window, at 50% usage => will hit 100% in another 3.375 days
		// That's 6.75 days total, still before 7-day reset
		result := PredictWeeklyDepletion(makeOAuth(50.0), now)

		assert.True(t, result.WillHitLimit, "should predict hitting limit")
		assert.False(t, result.DepletionTime.IsZero(), "should have depletion time")
		assert.True(t, result.DepletionTime.Before(resetTime), "depletion should be before reset")
	})

	t.Run("no warning when usage rate is safe", func(t *testing.T) {
		// 3.375 days in at only 10% => would hit 100% in ~30 more days, well after reset
		result := PredictWeeklyDepletion(makeOAuth(10.0), now)

		assert.False(t, result.WillHitLimit, "should not predict hitting limit at low usage")
	})

	t.Run("already at limit", func(t *testing.T) {
		result := PredictWeeklyDepletion(makeOAuth(100.0), now)

		assert.True(t, result.WillHitLimit)
		assert.Equal(t, now, result.DepletionTime, "depletion should be now")
	})

	t.Run("over limit", func(t *testing.T) {
		result := PredictWeeklyDepletion(makeOAuth(120.0), now)

		assert.True(t, result.WillHitLimit)
		assert.Equal(t, now, result.DepletionTime)
	})
}

func TestPredictModelWeeklyDepletion(t *testing.T) {
	now := time.Date(2026, 2, 11, 21, 0, 0, 0, time.UTC)
	resetTime := time.Date(2026, 2, 15, 12, 0, 0, 0, time.UTC) // 3.375 days elapsed
	resetStr := resetTime.Format(time.RFC3339Nano)

	fable := func(percent float64, resetsAt *string) oauth.Limit {
		return oauth.Limit{
			Kind:     oauth.KindWeeklyScoped,
			Percent:  percent,
			ResetsAt: resetsAt,
			Scope:    &oauth.LimitScope{Model: &oauth.LimitModel{DisplayName: "Fable"}},
		}
	}

	tests := []struct {
		name         string
		limit        oauth.Limit
		now          time.Time
		wantHit      bool
		wantHasTime  bool
		wantHasReset bool
	}{
		{
			name:         "hits limit before reset",
			limit:        fable(60, &resetStr),
			now:          now,
			wantHit:      true,
			wantHasTime:  true,
			wantHasReset: true,
		},
		{
			name:         "safe until reset",
			limit:        fable(20, &resetStr),
			now:          now,
			wantHit:      false,
			wantHasTime:  true,
			wantHasReset: true,
		},
		{
			name:         "already exhausted",
			limit:        fable(100, &resetStr),
			now:          now,
			wantHit:      true,
			wantHasTime:  true,
			wantHasReset: true,
		},
		{
			name:         "no extrapolation under 24h of data",
			limit:        fable(30, &resetStr),
			now:          resetTime.Add(-7*24*time.Hour + 5*time.Hour),
			wantHit:      false,
			wantHasTime:  false,
			wantHasReset: true,
		},
		{
			name:         "no reset time gives no prediction",
			limit:        fable(60, nil),
			now:          now,
			wantHit:      false,
			wantHasTime:  false,
			wantHasReset: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := PredictModelWeeklyDepletion(tt.limit, tt.now)
			assert.Equal(t, tt.limit.Percent, result.Utilisation)
			assert.Equal(t, tt.wantHit, result.WillHitLimit)
			assert.Equal(t, tt.wantHasTime, !result.DepletionTime.IsZero())
			assert.Equal(t, tt.wantHasReset, !result.ResetTime.IsZero())
			if tt.wantHit && tt.wantHasTime {
				assert.False(t, result.DepletionTime.After(resetTime))
			}
		})
	}
}

// A scoped limit's window is anchored to its own reset time, not the All
// Models window. The same percentage yields a different depletion time when
// the two windows are offset.
func TestPredictModelWeeklyDepletion_UsesOwnResetTime(t *testing.T) {
	now := time.Date(2026, 2, 11, 21, 0, 0, 0, time.UTC)
	allReset := time.Date(2026, 2, 15, 12, 0, 0, 0, time.UTC)
	fableReset := allReset.Add(36 * time.Hour)
	fableResetStr := fableReset.Format(time.RFC3339Nano)

	all := &oauth.UsageData{}
	all.SevenDay.Utilisation = 50
	all.SevenDay.ResetsAt = allReset.Format(time.RFC3339Nano)

	fable := oauth.Limit{
		Kind:     oauth.KindWeeklyScoped,
		Percent:  50,
		ResetsAt: &fableResetStr,
		Scope:    &oauth.LimitScope{Model: &oauth.LimitModel{DisplayName: "Fable"}},
	}

	allPred := PredictWeeklyDepletion(all, now)
	fablePred := PredictModelWeeklyDepletion(fable, now)

	assert.Equal(t, fableReset, fablePred.ResetTime)
	assert.NotEqual(t, allPred.DepletionTime, fablePred.DepletionTime)
	// Fable's window started later, so the same 50% was burnt faster
	assert.True(t, fablePred.DepletionTime.Before(allPred.DepletionTime))
}
