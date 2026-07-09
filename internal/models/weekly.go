package models

import "strings"

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

// WeeklyHoursForModel returns the plan's weekly hour allowance for a model,
// matched against the API's display name (e.g. "Sonnet", "Opus", "Fable").
// Returns 0 when we have no published hour figure for that model, in which case
// callers should present the raw utilisation percentage rather than invent one.
func WeeklyHoursForModel(plan, displayName string) float64 {
	limits := GetWeeklyLimits(plan)
	switch {
	case strings.Contains(strings.ToLower(displayName), "sonnet"):
		return limits.SonnetHours
	case strings.Contains(strings.ToLower(displayName), "opus"):
		return limits.OpusHours
	default:
		return 0
	}
}
