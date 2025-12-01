package models

import (
	"time"
)

// SessionBlock represents a 5-hour usage window
type SessionBlock struct {
	ID             string
	StartTime      time.Time
	EndTime        time.Time
	ActualEndTime  *time.Time // When session actually ended (for completed sessions)
	Entries        []UsageEntry
	TotalTokens    int // All tokens including cache (for cost calculations)
	DisplayTokens  int // Only input + output tokens (for UI display, matching Python)
	CostUSD        float64
	IsActive       bool
	IsGap          bool // True if this is a gap between sessions
	BurnRate       float64 // tokens per minute
	CostBurnRate   float64 // USD per minute
	PerModelStats  map[string]*ModelStats
	MessageCount   int
}

// Duration returns the session duration
func (sb *SessionBlock) Duration() time.Duration {
	if sb.ActualEndTime != nil {
		return sb.ActualEndTime.Sub(sb.StartTime)
	}
	return sb.EndTime.Sub(sb.StartTime)
}

// ElapsedDuration returns how much time has elapsed in this session
func (sb *SessionBlock) ElapsedDuration(now time.Time) time.Duration {
	if now.After(sb.EndTime) {
		return sb.Duration()
	}
	if now.Before(sb.StartTime) {
		return 0
	}
	return now.Sub(sb.StartTime)
}

// RemainingDuration returns how much time remains in this session
func (sb *SessionBlock) RemainingDuration(now time.Time) time.Duration {
	if now.After(sb.EndTime) {
		return 0
	}
	if now.Before(sb.StartTime) {
		return sb.Duration()
	}
	return sb.EndTime.Sub(now)
}

// Progress returns the progress through this session as a percentage (0-100)
func (sb *SessionBlock) Progress(now time.Time) float64 {
	elapsed := sb.ElapsedDuration(now).Minutes()
	total := sb.Duration().Minutes()
	if total == 0 {
		return 0
	}
	progress := (elapsed / total) * 100
	if progress > 100 {
		return 100
	}
	return progress
}

// AddEntry adds a usage entry to this session block
func (sb *SessionBlock) AddEntry(entry UsageEntry) {
	sb.Entries = append(sb.Entries, entry)
	sb.TotalTokens += entry.TotalTokens()       // All tokens (for cost calculations)
	sb.DisplayTokens += entry.DisplayTokens()   // Only input + output (for UI display)
	sb.MessageCount++

	// Update per-model stats
	if sb.PerModelStats == nil {
		sb.PerModelStats = make(map[string]*ModelStats)
	}

	normalisedModel := NormaliseModelName(entry.Model)
	if sb.PerModelStats[normalisedModel] == nil {
		sb.PerModelStats[normalisedModel] = &ModelStats{}
	}

	stats := sb.PerModelStats[normalisedModel]
	stats.InputTokens += entry.InputTokens
	stats.OutputTokens += entry.OutputTokens
	stats.CacheCreationTokens += entry.CacheCreationTokens
	stats.CacheReadTokens += entry.CacheReadTokens
	stats.CostUSD += entry.CostUSD
	stats.MessageCount++
}

// Limits represents plan-specific usage limits
type Limits struct {
	PlanName     string
	TokenLimit   int
	CostLimitUSD float64
	MessageLimit int
}

// PredefinedLimits contains the known plan limits
// Note: TokenLimit is 0 because Claude doesn't have per-5-hour-session token limits.
// The ~200k context window is per-conversation, not per-session.
var PredefinedLimits = map[string]Limits{
	"pro": {
		PlanName:     "Pro",
		TokenLimit:   0, // No per-session token limit
		CostLimitUSD: 18.0,
		MessageLimit: 250,
	},
	"max5": {
		PlanName:     "Max5",
		TokenLimit:   0, // No per-session token limit
		CostLimitUSD: 35.0,
		MessageLimit: 1000,
	},
	"max20": {
		PlanName:     "Max20",
		TokenLimit:   0, // No per-session token limit
		CostLimitUSD: 140.0,
		MessageLimit: 2000,
	},
}

// GetLimits returns the limits for a given plan
func GetLimits(plan string) Limits {
	if limits, ok := PredefinedLimits[plan]; ok {
		return limits
	}
	// Return Pro as default
	return PredefinedLimits["pro"]
}
