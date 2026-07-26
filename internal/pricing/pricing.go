package pricing

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sammcj/ccu/internal/models"
)

// fallbackModel is the pricing key used when a normalised model name is not
// found in ModelPricing. It MUST remain a valid key in ModelPricing; the
// init() check below fails fast if that invariant ever regresses (for example
// after a model rename in models.NormaliseModelName).
const fallbackModel = "claude-sonnet-4-6"

func init() {
	if _, ok := ModelPricing[fallbackModel]; !ok {
		panic(fmt.Sprintf("pricing: fallback model %q missing from ModelPricing", fallbackModel))
	}
}

// Pricing holds per-million token costs for a model
type Pricing struct {
	Input         float64
	Output        float64
	CacheCreation float64
	CacheRead     float64
}

// ModelPricing contains pricing for all known models (per 1M tokens in USD)
var ModelPricing = map[string]Pricing{
	// Fable 5 uses a new tokenizer (~30% more tokens for the same content than
	// Opus-tier models), so its token counts are not directly comparable to
	// other models' burn rates. Pricing maths is unaffected.
	"claude-fable-5": {
		Input:         10.00,
		Output:        50.00,
		CacheCreation: 12.50,
		CacheRead:     1.00,
	},
	// Mythos 5 is the same model as Fable 5 (Project Glasswing), same pricing
	"claude-mythos-5": {
		Input:         10.00,
		Output:        50.00,
		CacheCreation: 12.50,
		CacheRead:     1.00,
	},
	"claude-opus-5": {
		Input:         5.00,
		Output:        25.00,
		CacheCreation: 6.25,
		CacheRead:     0.50,
	},
	"claude-opus-4-8": {
		Input:         5.00,
		Output:        25.00,
		CacheCreation: 6.25,
		CacheRead:     0.50,
	},
	"claude-opus-4-7": {
		Input:         5.00,
		Output:        25.00,
		CacheCreation: 6.25,
		CacheRead:     0.50,
	},
	"claude-opus-4-6": {
		Input:         5.00,
		Output:        25.00,
		CacheCreation: 6.25,
		CacheRead:     0.50,
	},
	"claude-opus-4-5": {
		Input:         5.00,
		Output:        25.00,
		CacheCreation: 6.25,
		CacheRead:     0.50,
	},
	// Introductory rates, not the standing Sonnet tier - Anthropic is discounting
	// Sonnet 5 to $2/$10 for a limited period. FamilyPricing keeps the usual
	// $3/$15 so a future Sonnet does not inherit a promotional rate.
	"claude-sonnet-5": {
		Input:         2.00,
		Output:        10.00,
		CacheCreation: 2.50,
		CacheRead:     0.20,
	},
	"claude-sonnet-4-6": {
		Input:         3.00,
		Output:        15.00,
		CacheCreation: 3.75,
		CacheRead:     0.30,
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
	"claude-opus-4-1": {
		Input:         15.00,
		Output:        75.00,
		CacheCreation: 18.75,
		CacheRead:     1.50,
	},
	"claude-opus-4": {
		Input:         15.00,
		Output:        75.00,
		CacheCreation: 18.75,
		CacheRead:     1.50,
	},
	"claude-3-opus": {
		Input:         15.00,
		Output:        75.00,
		CacheCreation: 18.75,
		CacheRead:     1.50,
	},
	"claude-3-sonnet": {
		Input:         3.00,
		Output:        15.00,
		CacheCreation: 3.75,
		CacheRead:     0.30,
	},
	"claude-3-5-sonnet": {
		Input:         3.00,
		Output:        15.00,
		CacheCreation: 3.75,
		CacheRead:     0.30,
	},
	"claude-sonnet-4": {
		Input:         3.00,
		Output:        15.00,
		CacheCreation: 3.75,
		CacheRead:     0.30,
	},
	"claude-3-haiku": {
		Input:         0.25,
		Output:        1.25,
		CacheCreation: 0.30,
		CacheRead:     0.03,
	},
	"claude-3-5-haiku": {
		Input:         0.80,
		Output:        4.00,
		CacheCreation: 1.00,
		CacheRead:     0.08,
	},
}

// FamilyPricing holds the current rate for each model family, used when a model
// normalises to a version with no entry in ModelPricing. Anthropic prices every
// live tier within a family identically (Opus 4.5 through Opus 5 are all
// $5/$25), so a newly released version costs its family's rate far more often
// than it costs the global Sonnet fallback.
var FamilyPricing = map[string]Pricing{
	"fable":  {Input: 10.00, Output: 50.00, CacheCreation: 12.50, CacheRead: 1.00},
	"mythos": {Input: 10.00, Output: 50.00, CacheCreation: 12.50, CacheRead: 1.00},
	"opus":   {Input: 5.00, Output: 25.00, CacheCreation: 6.25, CacheRead: 0.50},
	"sonnet": {Input: 3.00, Output: 15.00, CacheCreation: 3.75, CacheRead: 0.30},
	"haiku":  {Input: 1.00, Output: 5.00, CacheCreation: 1.25, CacheRead: 0.10},
}

// Lookup resolves a raw model name to its rates, reporting whether the figures
// came from an exact ModelPricing entry. A false second return means the cost is
// an estimate: callers that surface cost to the user should say so rather than
// present a guessed rate as measured. The ladder is exact key → family rate →
// Sonnet fallback.
func Lookup(model string) (Pricing, bool) {
	normalised := models.NormaliseModelName(model)
	if p, ok := ModelPricing[normalised]; ok {
		return p, true
	}
	for _, family := range models.ModelFamilies {
		if strings.Contains(normalised, family) {
			return FamilyPricing[family], false
		}
	}
	return ModelPricing[fallbackModel], false
}

// apply converts token counts to USD at the given rates.
func (p Pricing) apply(input, output, cacheCreation, cacheRead int) float64 {
	cost := 0.0
	cost += float64(input) * p.Input / 1_000_000
	cost += float64(output) * p.Output / 1_000_000
	cost += float64(cacheCreation) * p.CacheCreation / 1_000_000
	cost += float64(cacheRead) * p.CacheRead / 1_000_000
	return cost
}

// CalculateCost calculates the cost of a usage entry in USD
func CalculateCost(entry models.UsageEntry) float64 {
	pricing, _ := Lookup(entry.Model)
	return pricing.apply(entry.InputTokens, entry.OutputTokens,
		entry.CacheCreationTokens, entry.CacheReadTokens)
}

// CalculateCostForTokens calculates cost for a specific model and token counts
func CalculateCostForTokens(model string, input, output, cacheCreation, cacheRead int) float64 {
	pricing, _ := Lookup(model)
	return pricing.apply(input, output, cacheCreation, cacheRead)
}

// EstimatedModels returns the normalised names of any supplied models CCU has no
// exact rate for, sorted for stable display. The UI uses this to mark cost
// figures as estimates instead of silently presenting a fallback rate as fact.
func EstimatedModels(rawModels []string) []string {
	seen := make(map[string]bool)
	var estimated []string
	for _, m := range rawModels {
		if _, exact := Lookup(m); exact {
			continue
		}
		normalised := models.NormaliseModelName(m)
		if seen[normalised] {
			continue
		}
		seen[normalised] = true
		estimated = append(estimated, normalised)
	}
	sort.Strings(estimated)
	return estimated
}

// GetPricingSource returns the pricing source description
func GetPricingSource() string {
	return "Anthropic API pricing (https://www.anthropic.com/pricing)"
}
