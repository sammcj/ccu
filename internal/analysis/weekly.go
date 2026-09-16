package analysis

import (
	"time"

	"github.com/sammcj/ccu/internal/oauth"
)

// WeeklyPrediction holds the prediction for when the weekly limit will be hit
type WeeklyPrediction struct {
	DepletionTime time.Time // When the limit will be hit (zero if won't hit before reset)
	ResetTime     time.Time // When the weekly window resets
	WillHitLimit  bool      // True if limit will be hit before reset
	Utilisation   float64   // Current utilisation percentage
}

// PredictWeeklyDepletion predicts when the "All Models" weekly limit will be hit
// based on the actual weekly consumption rate (not the momentary session burn rate).
func PredictWeeklyDepletion(oauthData *oauth.UsageData, now time.Time) WeeklyPrediction {
	return predictWeeklyDepletion(oauthData.SevenDay.Utilisation, oauthData.SevenDay.ResetsAt, now)
}

// PredictModelWeeklyDepletion predicts when a per-model weekly limit (e.g. Fable)
// will be hit. Each scoped limit carries its own reset time, which can differ
// from the All Models window, so the prediction is anchored to the limit's own
// window rather than SevenDay.ResetsAt.
func PredictModelWeeklyDepletion(limit oauth.Limit, now time.Time) WeeklyPrediction {
	if limit.ResetsAt == nil {
		return WeeklyPrediction{Utilisation: limit.Percent}
	}
	return predictWeeklyDepletion(limit.Percent, *limit.ResetsAt, now)
}

// predictWeeklyDepletion extrapolates a weekly limit's depletion time from its
// utilisation and reset time.
//
// The calculation uses the real average burn rate over the weekly window:
// - Weekly window starts 7 days before reset time
// - Burn rate = utilisation% / hours elapsed since window start
// - This reflects actual usage patterns, not momentary session intensity
func predictWeeklyDepletion(utilisation float64, resetsAt string, now time.Time) WeeklyPrediction {
	prediction := WeeklyPrediction{
		Utilisation: utilisation,
	}

	// Parse reset time
	resetTime, err := oauth.ParseResetTime(resetsAt)
	if err != nil {
		return prediction
	}
	prediction.ResetTime = resetTime

	// Already at or over limit
	if prediction.Utilisation >= 100 {
		prediction.WillHitLimit = true
		prediction.DepletionTime = now
		return prediction
	}

	// Calculate weekly window start (7 days before reset)
	weekStart := resetTime.Add(-7 * 24 * time.Hour)

	// Calculate hours elapsed since week started
	hoursElapsed := now.Sub(weekStart).Hours()
	if hoursElapsed <= 0 {
		return prediction
	}

	// Don't extrapolate from less than 24 hours of data.
	// With only a few hours elapsed, the average burn rate is dominated by
	// active usage and doesn't account for sleep/idle time, producing
	// wildly aggressive predictions.
	if hoursElapsed < 24 {
		return prediction
	}

	// Calculate actual weekly burn rate from real usage
	// This is % per hour based on actual consumption over the week
	weeklyBurnRatePerHour := prediction.Utilisation / hoursElapsed

	if weeklyBurnRatePerHour <= 0 {
		return prediction
	}

	// Calculate time to depletion at current average rate
	remainingPercent := 100 - prediction.Utilisation
	hoursToDepletion := remainingPercent / weeklyBurnRatePerHour
	prediction.DepletionTime = now.Add(time.Duration(hoursToDepletion * float64(time.Hour)))

	// Check if depletion is before reset
	prediction.WillHitLimit = prediction.DepletionTime.Before(prediction.ResetTime)

	return prediction
}
