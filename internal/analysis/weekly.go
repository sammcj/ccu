package analysis

import (
	"strings"
	"time"

	"github.com/sammcj/ccu/internal/models"
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
