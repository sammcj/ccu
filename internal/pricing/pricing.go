package pricing

import (
	"fmt"
	"sort"
	"time"

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
	// LongContext, when set, replaces every rate above for a request whose
	// prompt (input + cache write + cache read tokens) exceeds
	// LongContextThreshold. Haiku 5.5 is priced this way.
	LongContextThreshold int      `json:",omitempty"`
	LongContext          *Pricing `json:",omitempty"`
	// FastMultiplier scales every rate for a fast mode request. Zero means the
	// model has no fast mode, so its fast requests bill at standard rates.
	FastMultiplier float64 `json:",omitempty"`
}

// ModelPricing contains pricing for all known models (per 1M tokens in USD)
var ModelPricing = map[string]Pricing{
	// Fable 5.1 is priced as Fable 5 except cache reads, which dropped to $0.25.
	"claude-fable-5-1": {
		Input:         10.00,
		Output:        50.00,
		CacheCreation: 12.50,
		CacheRead:     0.25,
	},
	// Mythos 5.1 is the same model as Fable 5.1 (Project Glasswing), same pricing
	"claude-mythos-5-1": {
		Input:         10.00,
		Output:        50.00,
		CacheCreation: 12.50,
		CacheRead:     0.25,
	},
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
	"claude-opus-5-5": {
		Input:          4.00,
		Output:         20.00,
		CacheCreation:  5.00,
		CacheRead:      0.20,
		FastMultiplier: 2,
	},
	"claude-opus-5": {
		Input:          5.00,
		Output:         25.00,
		CacheCreation:  6.25,
		CacheRead:      0.50,
		FastMultiplier: 2,
	},
	"claude-opus-4-8": {
		Input:          5.00,
		Output:         25.00,
		CacheCreation:  6.25,
		CacheRead:      0.50,
		FastMultiplier: 2,
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
	// Cache reads are 0.05x input on Sonnet 5.5, half the usual 0.1x
	"claude-sonnet-5-5": {
		Input:         2.00,
		Output:        10.00,
		CacheCreation: 2.50,
		CacheRead:     0.10,
	},
	// Introductory rates - Anthropic discounts Sonnet 5 to $2/$10 for a limited
	// period, so these may rise. Sonnet 5.5 is $2/$10 at standard pricing.
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
	"claude-haiku-5-5": {
		Input:                0.10,
		Output:               0.50,
		CacheCreation:        0.125,
		CacheRead:            0.01,
		LongContextThreshold: 100_000,
		LongContext:          &Pricing{Input: 0.50, Output: 2.50, CacheCreation: 0.625, CacheRead: 0.05},
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
// normalises to a version with no entry in ModelPricing. A new version usually
// keeps or undercuts its family's newest rate, so that rate is a far better
// estimate than the global Sonnet fallback.
var FamilyPricing = map[string]Pricing{
	"fable":  {Input: 10.00, Output: 50.00, CacheCreation: 12.50, CacheRead: 0.25},
	"mythos": {Input: 10.00, Output: 50.00, CacheCreation: 12.50, CacheRead: 0.25},
	"opus":   ModelPricing["claude-opus-5-5"],
	"sonnet": ModelPricing["claude-sonnet-5-5"],
	"haiku":  ModelPricing["claude-haiku-5-5"],
}

// Source records where Lookup found a model's rates. Anything other than
// SourceExact is an estimate, and callers surfacing cost to the user should say
// so rather than present a guessed rate as measured.
type Source int

const (
	// SourceExact means the model has its own published rate, from ModelPricing
	// or Anthropic's pricing page.
	SourceExact Source = iota
	// SourceFamily means the family is known but this version has no published
	// rate, so the family's standing rate was used.
	SourceFamily
	// SourceFallback means nothing about the model is known and the global
	// Sonnet fallback was used.
	SourceFallback
)

// Lookup resolves a raw model name to its rates and reports where they came
// from. The ladder is exact key → published page → family rate → Sonnet fallback.
func Lookup(model string) (Pricing, Source) {
	normalised := models.NormaliseModelName(model)
	if p, ok := ModelPricing[normalised]; ok {
		return p, SourceExact
	}
	if family := models.FamilyOf(normalised); family != "" {
		// The page only lists Claude families, so unknown models skip the fetch
		if p, ok := published.lookup(normalised); ok {
			return p, SourceExact
		}
		return FamilyPricing[family], SourceFamily
	}
	return ModelPricing[fallbackModel], SourceFallback
}

// cacheWrite1hMultiplier is Anthropic's 1-hour cache write price as a multiple
// of base input. It holds for every model and tier on the pricing page, and
// -check-models compares it against upstream.
const cacheWrite1hMultiplier = 2.0

// CacheCreation1h is the per-million rate for writes to the 1-hour cache.
// CacheCreation is the 5-minute rate.
func (p Pricing) CacheCreation1h() float64 {
	return p.Input * cacheWrite1hMultiplier
}

// cost converts one request's tokens to USD. It must be given a single request:
// a long-context tier is chosen by that request's prompt length, so summed
// counts would pick the wrong tier.
func (p Pricing) cost(e models.UsageEntry) float64 {
	fast := p.FastMultiplier
	if p.LongContext != nil && e.InputTokens+e.CacheCreationTokens+e.CacheReadTokens > p.LongContextThreshold {
		p = *p.LongContext
	}
	// Cache multipliers stack on top of fast mode, so every rate scales
	if e.FastMode && fast > 0 {
		p.Input *= fast
		p.Output *= fast
		p.CacheCreation *= fast
		p.CacheRead *= fast
	}
	write1h := e.CacheCreation1hTokens
	write5m := e.CacheCreationTokens - write1h
	cost := 0.0
	cost += float64(e.InputTokens) * p.Input / 1_000_000
	cost += float64(e.OutputTokens) * p.Output / 1_000_000
	cost += float64(write5m) * p.CacheCreation / 1_000_000
	cost += float64(write1h) * p.CacheCreation1h() / 1_000_000
	cost += float64(e.CacheReadTokens) * p.CacheRead / 1_000_000
	return cost
}

// CalculateCost calculates the cost of a usage entry in USD
func CalculateCost(entry models.UsageEntry) float64 {
	pricing, _ := Lookup(entry.Model)
	return pricing.cost(entry)
}

// cacheTTL5m and cacheTTL1h are the two prompt cache lifetimes.
const (
	cacheTTL5m = 5 * time.Minute
	cacheTTL1h = time.Hour
)

// CostWith5mCache is what a request would have cost with only the 5-minute
// cache, given the time since the previous request in the same conversation.
// A read more than 5 minutes but at most an hour after it was kept alive by the
// 1-hour cache, so under the 5-minute cache those tokens are written again.
// Reads after a longer gap came from another source and are left as reads.
func CostWith5mCache(e models.UsageEntry, sincePrevious time.Duration) float64 {
	if sincePrevious > cacheTTL5m && sincePrevious <= cacheTTL1h {
		e.CacheCreationTokens += e.CacheReadTokens
		e.CacheReadTokens = 0
	}
	e.CacheCreation1hTokens = 0
	return CalculateCost(e)
}

// Estimate names a model CCU has no exact rate for. Family is the family whose
// standing rate stood in, or empty when the model is unrecognised and the Sonnet
// fallback applied.
type Estimate struct {
	Model  string
	Family string
}

// EstimatedModels returns the normalised names of any supplied models CCU has no
// exact rate for, sorted by model for stable display. The UI uses this to mark
// cost figures as estimates instead of silently presenting a fallback rate as
// fact, and to say which family rate stood in where one did.
func EstimatedModels(rawModels []string) []Estimate {
	seen := make(map[string]bool)
	var estimated []Estimate
	for _, m := range rawModels {
		if _, source := Lookup(m); source == SourceExact {
			continue
		}
		normalised := models.NormaliseModelName(m)
		if seen[normalised] {
			continue
		}
		seen[normalised] = true
		estimated = append(estimated, Estimate{Model: normalised, Family: models.FamilyOf(normalised)})
	}
	sort.Slice(estimated, func(i, j int) bool { return estimated[i].Model < estimated[j].Model })
	return estimated
}

// GetPricingSource returns the pricing source description
func GetPricingSource() string {
	return "Anthropic API pricing (https://www.anthropic.com/pricing)"
}
