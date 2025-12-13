package analysis

import (
	"strings"
	"time"

	"github.com/sammcj/ccu/internal/models"
	"github.com/sammcj/ccu/internal/oauth"
)

// CalculateWeeklyUsage calculates usage over the last 7 days using pre-computed
// session blocks to avoid redundant computation.
func CalculateWeeklyUsage(blocks []models.SessionBlock, plan string, now time.Time) models.WeeklyUsage {
	weekAgo := now.AddDate(0, 0, -7)

	usage := models.WeeklyUsage{
		StartDate: weekAgo,
		EndDate:   now,
	}

	// Filter session blocks for the last 7 days
	var weeklyBlocks []models.SessionBlock
	for _, block := range blocks {
		// Include blocks that started within the last 7 days
		if !block.StartTime.Before(weekAgo) {
			weeklyBlocks = append(weeklyBlocks, block)
		}
	}

	if len(weeklyBlocks) == 0 {
		return usage
	}

	sessionBlocks := weeklyBlocks

	// Sum tokens from each non-gap session block
	// Use DisplayTokens (input + output only) to match UI display and avoid cache token inflation
	for _, block := range sessionBlocks {
		if block.IsGap {
			continue
		}

		// Sum display tokens for this session (excludes cache tokens)
		tokens := block.DisplayTokens

		usage.TotalTokens += tokens

		// Categorise by model type using DisplayTokens to match UI
		for model, stats := range block.PerModelStats {
			modelTokens := stats.DisplayTokens()

			if isSonnetModel(model) {
				usage.SonnetTokens += modelTokens
			} else if isOpusModel(model) {
				usage.OpusTokens += modelTokens
			}
		}
	}

	// Convert to estimated hours using plan-specific rates
	sonnetRate, opusRate := models.GetTokensPerHour(plan)
	usage.SonnetHours = float64(usage.SonnetTokens) / sonnetRate
	usage.OpusHours = float64(usage.OpusTokens) / opusRate

	// Get limits for plan
	limits := models.GetWeeklyLimits(plan)
	usage.SonnetLimit = limits.SonnetHours
	usage.OpusLimit = limits.OpusHours

	// Calculate percentages
	if usage.SonnetLimit > 0 {
		usage.SonnetPercent = (usage.SonnetHours / usage.SonnetLimit) * 100
	}
	if usage.OpusLimit > 0 {
		usage.OpusPercent = (usage.OpusHours / usage.OpusLimit) * 100
	}

	return usage
}


// isSonnetModel checks if a model is a Sonnet variant
func isSonnetModel(model string) bool {
	return strings.Contains(strings.ToLower(model), "sonnet")
}

// isOpusModel checks if a model is an Opus variant
func isOpusModel(model string) bool {
	return strings.Contains(strings.ToLower(model), "opus")
}

// IsWeeklyLimitApproaching returns true if weekly usage is >80%
func IsWeeklyLimitApproaching(usage models.WeeklyUsage) bool {
	return usage.SonnetPercent > 80 || usage.OpusPercent > 80
}

// IsWeeklyLimitExceeded returns true if weekly usage is >100%
func IsWeeklyLimitExceeded(usage models.WeeklyUsage) bool {
	return usage.SonnetPercent > 100 || usage.OpusPercent > 100
}

// EstimateHoursRemaining estimates how many hours remain in the weekly limit
func EstimateHoursRemaining(usage models.WeeklyUsage) (sonnetHours, opusHours float64) {
	sonnetRemaining := usage.SonnetLimit - usage.SonnetHours
	if sonnetRemaining < 0 {
		sonnetRemaining = 0
	}

	opusRemaining := usage.OpusLimit - usage.OpusHours
	if opusRemaining < 0 {
		opusRemaining = 0
	}

	return sonnetRemaining, opusRemaining
}

// WeeklyPrediction holds the prediction for when the weekly limit will be hit
type WeeklyPrediction struct {
	DepletionTime time.Time // When the limit will be hit (zero if won't hit before reset)
	ResetTime     time.Time // When the weekly window resets
	WillHitLimit  bool      // True if limit will be hit before reset
	Utilisation   float64   // Current utilisation percentage
}

// PredictWeeklyDepletion predicts when the "All Models" weekly limit will be hit
// based on the actual weekly consumption rate (not the momentary session burn rate).
//
// The calculation uses the real average burn rate over the weekly window:
// - Weekly window starts 7 days before reset time
// - Burn rate = utilisation% / hours elapsed since window start
// - This reflects actual usage patterns, not momentary session intensity
func PredictWeeklyDepletion(oauthData *oauth.UsageData, _ float64, _ float64, now time.Time) WeeklyPrediction {
	prediction := WeeklyPrediction{
		Utilisation: oauthData.SevenDay.Utilisation,
	}

	// Parse reset time
	resetTime, err := oauth.ParseResetTime(oauthData.SevenDay.ResetsAt)
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
