package analysis

import (
	"sort"

	"github.com/sammcj/ccu/internal/models"
)

// P90Config holds configuration for P90 calculation
type P90Config struct {
	CommonLimits     []int
	LimitThreshold   float64
	DefaultMinLimit  int
}

// DefaultP90Config returns the default P90 configuration
func DefaultP90Config() P90Config {
	return P90Config{
		CommonLimits:    []int{19000, 88000, 220000}, // Pro, Max5, Max20
		LimitThreshold:  0.95,                        // 95% of limit
		DefaultMinLimit: 19000,                       // Pro limit as minimum
	}
}

// CalculateP90Limit calculates the P90 limit from session blocks
// See Appendix B for algorithm details
func CalculateP90Limit(blocks []models.SessionBlock, config P90Config) int {
	// Step 1: Find sessions that hit known limits
	var hitSessions []int

	for _, block := range blocks {
		if block.IsGap || block.IsActive {
			continue
		}

		// Use TotalTokens (not DisplayTokens) for limit detection because plan limits
		// are based on total cost, which includes cache tokens
		tokens := block.TotalTokens

		// Check if tokens are close to known limits (within threshold)
		for _, limit := range config.CommonLimits {
			if float64(tokens) >= float64(limit)*config.LimitThreshold {
				hitSessions = append(hitSessions, tokens)
				break
			}
		}
	}

	// Step 2: If no limit hits, use all completed sessions
	if len(hitSessions) == 0 {
		for _, block := range blocks {
			// Use TotalTokens for cost-based limit detection
			if !block.IsGap && !block.IsActive && block.TotalTokens > 0 {
				hitSessions = append(hitSessions, block.TotalTokens)
			}
		}
	}

	// Step 3: Return P90 (90th percentile)
	if len(hitSessions) == 0 {
		return config.DefaultMinLimit
	}

	p90 := quantile(hitSessions, 0.90)
	if p90 < config.DefaultMinLimit {
		return config.DefaultMinLimit
	}

	return p90
}

// quantile calculates the nth quantile of a slice of integers
func quantile(data []int, q float64) int {
	if len(data) == 0 {
		return 0
	}

	// Sort data
	sorted := make([]int, len(data))
	copy(sorted, data)
	sort.Ints(sorted)

	// Calculate index
	index := int(float64(len(sorted)-1) * q)
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}

	return sorted[index]
}

// DetectPlan attempts to detect the user's plan based on usage patterns
func DetectPlan(blocks []models.SessionBlock) string {
	config := DefaultP90Config()
	p90Limit := CalculateP90Limit(blocks, config)

	// Detect based on P90 limit
	switch {
	case p90Limit >= 200000:
		return "max20"
	case p90Limit >= 80000:
		return "max5"
	case p90Limit >= 15000:
		return "pro"
	default:
		return "custom"
	}
}

// ShouldSwitchToCustom determines if we should switch from a predefined plan to custom
// This happens when actual usage consistently exceeds the plan limit
func ShouldSwitchToCustom(blocks []models.SessionBlock, currentPlan string) bool {
	if currentPlan == "custom" {
		return false
	}

	limits := models.GetLimits(currentPlan)
	exceedCount := 0
	totalSessions := 0

	for _, block := range blocks {
		if block.IsGap || block.IsActive {
			continue
		}

		totalSessions++
		// Use TotalTokens for cost-based limit comparison
		if block.TotalTokens > limits.TokenLimit {
			exceedCount++
		}
	}

	// Switch to custom if >30% of sessions exceed the plan limit
	if totalSessions > 0 && float64(exceedCount)/float64(totalSessions) > 0.3 {
		return true
	}

	return false
}
