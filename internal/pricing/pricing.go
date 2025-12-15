package pricing

import "github.com/sammcj/ccu/internal/models"

// Pricing holds per-million token costs for a model
type Pricing struct {
	Input         float64
	Output        float64
	CacheCreation float64
	CacheRead     float64
}

// ModelPricing contains pricing for all known models (per 1M tokens in USD)
var ModelPricing = map[string]Pricing{
	"claude-opus-4-5": {
		Input:         5.00,
		Output:        25.00,
		CacheCreation: 6.25,
		CacheRead:     0.50,
	},
	"claude-sonnet-4-5": {
		Input:         3.00,
		Output:        15.00,
		CacheCreation: 3.75,
		CacheRead:     0.30,
	},
	"claude-haiku-4-5": {
		Input:         1.00,
		Output:        5.00,
		CacheCreation: 1.25,
		CacheRead:     0.10,
	},
	"claude-opus-4": { // Legacy, should never be used as of October 2025
		Input:         15.00,
		Output:        75.00,
		CacheCreation: 18.75,
		CacheRead:     1.50,
	},
	"claude-3-opus": { // Legacy, should never be used as of October 2025
		Input:         15.00,
		Output:        75.00,
		CacheCreation: 18.75,
		CacheRead:     1.50,
	},
	"claude-3-sonnet": { // Legacy, should never be used as of October 2025
		Input:         3.00,
		Output:        15.00,
		CacheCreation: 3.75,
		CacheRead:     0.30,
	},
	"claude-3-5-sonnet": { // Legacy, should never be used as of October 2025
		Input:         3.00,
		Output:        15.00,
		CacheCreation: 3.75,
		CacheRead:     0.30,
	},
	"claude-sonnet-4": { // Legacy, should never be used as of October 2025
		Input:         3.00,
		Output:        15.00,
		CacheCreation: 3.75,
		CacheRead:     0.30,
	},
	"claude-3-haiku": { // Legacy, should never be used as of October 2025
		Input:         0.25,
		Output:        1.25,
		CacheCreation: 0.30,
		CacheRead:     0.03,
	},
	"claude-3-5-haiku": { // Legacy, should never be used as of October 2025
		Input:         0.80,
		Output:        4.00,
		CacheCreation: 1.00,
		CacheRead:     0.08,
	},
}

// CalculateCost calculates the cost of a usage entry in USD
func CalculateCost(entry models.UsageEntry) float64 {
	normalisedModel := models.NormaliseModelName(entry.Model)
	pricing, ok := ModelPricing[normalisedModel]
	if !ok {
		// Use Sonnet pricing as default for unknown models
		pricing = ModelPricing["claude-sonnet-4"]
	}

	cost := 0.0
	cost += float64(entry.InputTokens) * pricing.Input / 1_000_000
	cost += float64(entry.OutputTokens) * pricing.Output / 1_000_000
	cost += float64(entry.CacheCreationTokens) * pricing.CacheCreation / 1_000_000
	cost += float64(entry.CacheReadTokens) * pricing.CacheRead / 1_000_000

	return cost
}

// CalculateCostForTokens calculates cost for a specific model and token counts
func CalculateCostForTokens(model string, input, output, cacheCreation, cacheRead int) float64 {
	normalisedModel := models.NormaliseModelName(model)
	pricing, ok := ModelPricing[normalisedModel]
	if !ok {
		pricing = ModelPricing["claude-sonnet-4"]
	}

	cost := 0.0
	cost += float64(input) * pricing.Input / 1_000_000
	cost += float64(output) * pricing.Output / 1_000_000
	cost += float64(cacheCreation) * pricing.CacheCreation / 1_000_000
	cost += float64(cacheRead) * pricing.CacheRead / 1_000_000

	return cost
}

// GetPricingSource returns the pricing source description
func GetPricingSource() string {
	return "Anthropic API pricing (https://www.anthropic.com/pricing)"
}
