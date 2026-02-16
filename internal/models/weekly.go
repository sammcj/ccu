package models

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

