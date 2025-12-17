package models

import "time"

// WeeklyUsage tracks 7-day rolling consumption
type WeeklyUsage struct {
	StartDate time.Time
	EndDate   time.Time

	// Token consumption
	TotalTokens  int
	SonnetTokens int
	OpusTokens   int

	// Estimated hours
	SonnetHours float64
	OpusHours   float64

	// Limits for current plan
	SonnetLimit float64
	OpusLimit   float64

	// Progress
	SonnetPercent float64 // 0-100
	OpusPercent   float64 // 0-100
}

// WeeklyLimits defines weekly limits by plan
type WeeklyLimits struct {
	SonnetHours float64
	OpusHours   float64
}

// PredefinedWeeklyLimits contains known weekly limits
// Note: Opus weekly limits are currently disabled as Anthropic is not enforcing them.
// Set OpusHours > 0 when/if Anthropic re-enables Opus weekly limits.
var PredefinedWeeklyLimits = map[string]WeeklyLimits{
	"pro": {
		SonnetHours: 60, // Using mid-range of 40-80
		OpusHours:   0,  // Pro doesn't have Opus access
	},
	"max5": {
		SonnetHours: 210, // Using mid-range of 140-280
		OpusHours:   0,   // Opus weekly limits not currently enforced by Anthropic
	},
	"max20": {
		SonnetHours: 360, // Using mid-range of 240-480
		OpusHours:   0,   // Opus weekly limits not currently enforced by Anthropic
	},
}

// GetWeeklyLimits returns the weekly limits for a given plan
func GetWeeklyLimits(plan string) WeeklyLimits {
	if limits, ok := PredefinedWeeklyLimits[plan]; ok {
		return limits
	}
	// Return Pro as default
	return PredefinedWeeklyLimits["pro"]
}

// GetTokensPerHour returns plan-specific token rates for hour estimation
// These are estimates based on typical usage patterns, not hard limits
func GetTokensPerHour(plan string) (sonnet, opus float64) {
	// Base estimates on session limits divided by session duration
	limits := GetLimits(plan)
	sessionHours := 5.0

	// Estimate Sonnet rate from plan's token limit
	// Use 60% of max throughput as average (accounting for thinking time, pauses, etc.)
	sonnetRate := (float64(limits.TokenLimit) / sessionHours) * 0.6

	// Opus is typically ~10x more expensive, so assume ~5x fewer tokens for same cost
	opusRate := sonnetRate * 0.5

	// Apply minimums to avoid division by zero
	if sonnetRate < 1000 {
		sonnetRate = 10000 // Fallback estimate
	}
	if opusRate < 500 {
		opusRate = 5000 // Fallback estimate
	}

	return sonnetRate, opusRate
}
