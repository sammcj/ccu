package pricing

import (
	"testing"
	"time"

	"github.com/sammcj/ccu/internal/models"
)

func TestCalculateCost(t *testing.T) {
	tests := []struct {
		name  string
		entry models.UsageEntry
		want  float64
	}{
		{
			name: "fable 5 with cache",
			entry: models.UsageEntry{
				Timestamp:           time.Now(),
				InputTokens:         1000,
				OutputTokens:        500,
				CacheCreationTokens: 200,
				CacheReadTokens:     300,
				Model:               "claude-fable-5",
			},
			// (1000 * 10 / 1M) + (500 * 50 / 1M) + (200 * 12.50 / 1M) + (300 * 1.00 / 1M)
			// = 0.01 + 0.025 + 0.0025 + 0.0003 = 0.0378
			want: 0.0378,
		},
		{
			name: "opus 4.8 with cache",
			entry: models.UsageEntry{
				Timestamp:           time.Now(),
				InputTokens:         1000,
				OutputTokens:        500,
				CacheCreationTokens: 200,
				CacheReadTokens:     300,
				Model:               "claude-opus-4-8",
			},
			// Same rates as Opus 4.6, NOT the legacy claude-opus-4 rates
			// (1000 * 5 / 1M) + (500 * 25 / 1M) + (200 * 6.25 / 1M) + (300 * 0.5 / 1M)
			// = 0.005 + 0.0125 + 0.00125 + 0.00015 = 0.0189
			want: 0.0189,
		},
		{
			name: "sonnet basic usage",
			entry: models.UsageEntry{
				Timestamp:    time.Now(),
				InputTokens:  1000,
				OutputTokens: 500,
				Model:        "claude-sonnet-4",
			},
			// (1000 * 3.00 / 1M) + (500 * 15.00 / 1M) = 0.003 + 0.0075 = 0.0105
			want: 0.0105,
		},
		{
			name: "opus 4 legacy with cache",
			entry: models.UsageEntry{
				Timestamp:           time.Now(),
				InputTokens:         1000,
				OutputTokens:        500,
				CacheCreationTokens: 200,
				CacheReadTokens:     300,
				Model:               "claude-opus-4",
			},
			// (1000 * 15 / 1M) + (500 * 75 / 1M) + (200 * 18.75 / 1M) + (300 * 1.5 / 1M)
			// = 0.015 + 0.0375 + 0.00375 + 0.00045 = 0.0567
			want: 0.0567,
		},
		{
			name: "opus 4.6 with cache",
			entry: models.UsageEntry{
				Timestamp:           time.Now(),
				InputTokens:         1000,
				OutputTokens:        500,
				CacheCreationTokens: 200,
				CacheReadTokens:     300,
				Model:               "claude-opus-4-6",
			},
			// (1000 * 5 / 1M) + (500 * 25 / 1M) + (200 * 6.25 / 1M) + (300 * 0.5 / 1M)
			// = 0.005 + 0.0125 + 0.00125 + 0.00015 = 0.0189
			want: 0.0189,
		},
		{
			name: "sonnet 4.6 basic usage",
			entry: models.UsageEntry{
				Timestamp:    time.Now(),
				InputTokens:  1000,
				OutputTokens: 500,
				Model:        "claude-sonnet-4-6",
			},
			// (1000 * 3.00 / 1M) + (500 * 15.00 / 1M) = 0.003 + 0.0075 = 0.0105
			want: 0.0105,
		},
		{
			// Regression: Opus 5 used to normalise to claude-opus-4 and bill at
			// $15/$75, overstating every Opus 5 cost by 3x.
			name: "opus 5 with cache",
			entry: models.UsageEntry{
				Timestamp:           time.Now(),
				InputTokens:         1000,
				OutputTokens:        500,
				CacheCreationTokens: 200,
				CacheReadTokens:     300,
				Model:               "claude-opus-5",
			},
			// (1000 * 5 / 1M) + (500 * 25 / 1M) + (200 * 6.25 / 1M) + (300 * 0.5 / 1M)
			// = 0.005 + 0.0125 + 0.00125 + 0.00015 = 0.0189
			want: 0.0189,
		},
		{
			name: "opus 5 with 1m context suffix",
			entry: models.UsageEntry{
				Timestamp:    time.Now(),
				InputTokens:  1000,
				OutputTokens: 500,
				Model:        "claude-opus-5[1m]",
			},
			// (1000 * 5 / 1M) + (500 * 25 / 1M) = 0.005 + 0.0125 = 0.0175
			want: 0.0175,
		},
		{
			name: "sonnet 5 basic usage",
			entry: models.UsageEntry{
				Timestamp:    time.Now(),
				InputTokens:  1000,
				OutputTokens: 500,
				Model:        "claude-sonnet-5",
			},
			// Introductory rates, below the standing Sonnet tier.
			// (1000 * 2.00 / 1M) + (500 * 10.00 / 1M) = 0.002 + 0.005 = 0.007
			want: 0.007,
		},
		{
			// Anthropic publishes some Claude 4 IDs with the version before the
			// family. This one used to be read as an unversioned "opus" and priced
			// as the newest Opus, three tiers off its actual rate.
			name: "opus 4 with the version before the family",
			entry: models.UsageEntry{
				Timestamp:    time.Now(),
				InputTokens:  1000,
				OutputTokens: 500,
				Model:        "claude-4-opus-20250514",
			},
			// (1000 * 15 / 1M) + (500 * 75 / 1M) = 0.015 + 0.0375 = 0.0525
			want: 0.0525,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateCost(tt.entry)
			// Allow small floating point differences
			if diff := got - tt.want; diff > 0.0001 || diff < -0.0001 {
				t.Errorf("CalculateCost() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalculateCostForTokens(t *testing.T) {
	tests := []struct {
		name          string
		model         string
		input         int
		output        int
		cacheCreation int
		cacheRead     int
		want          float64
	}{
		{
			name:   "haiku basic",
			model:  "claude-3-haiku",
			input:  10000,
			output: 5000,
			// (10000 * 0.25 / 1M) + (5000 * 1.25 / 1M) = 0.0025 + 0.00625 = 0.00875
			want: 0.00875,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateCostForTokens(tt.model, tt.input, tt.output, tt.cacheCreation, tt.cacheRead)
			if diff := got - tt.want; diff > 0.0001 || diff < -0.0001 {
				t.Errorf("CalculateCostForTokens() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestLookupFallbackLadder covers the three tiers: exact entry, family rate for a
// version we have not published, and the Sonnet fallback for a non-Claude model.
// The second return distinguishes a measured rate from an estimated one.
func TestLookupFallbackLadder(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		want      Pricing
		wantExact bool
	}{
		{
			name:      "known model uses its own entry",
			model:     "claude-opus-5",
			want:      ModelPricing["claude-opus-5"],
			wantExact: true,
		},
		{
			// An unreleased Opus version must cost Opus rates, not Sonnet rates.
			name:      "unreleased opus version falls back to the opus family rate",
			model:     "claude-opus-6",
			want:      FamilyPricing["opus"],
			wantExact: false,
		},
		{
			name:      "unreleased haiku version falls back to the haiku family rate",
			model:     "claude-haiku-6",
			want:      FamilyPricing["haiku"],
			wantExact: false,
		},
		{
			name:      "legacy claude 3 name still resolves to its own entry",
			model:     "claude-3-opus-20240229",
			want:      ModelPricing["claude-3-opus"],
			wantExact: true,
		},
		{
			name:      "non-claude model falls back to sonnet",
			model:     "gemma-4-26b-a4b-it-5bit",
			want:      ModelPricing[fallbackModel],
			wantExact: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, exact := Lookup(tt.model)
			if got != tt.want {
				t.Errorf("Lookup(%q) = %+v, want %+v", tt.model, got, tt.want)
			}
			if exact != tt.wantExact {
				t.Errorf("Lookup(%q) exact = %v, want %v", tt.model, exact, tt.wantExact)
			}
		})
	}
}

// TestEveryFamilyHasFallbackPricing stops a new family being added to
// models.ModelFamilies without a rate, which would otherwise silently resolve to
// the zero Pricing value and report every request as free.
func TestEveryFamilyHasFallbackPricing(t *testing.T) {
	for _, family := range models.ModelFamilies {
		if FamilyPricing[family] == (Pricing{}) {
			t.Errorf("family %q has no entry in FamilyPricing", family)
		}
	}
}

// TestEstimatedModels checks the UI notice only names models CCU cannot price
// exactly, deduplicated and sorted.
func TestEstimatedModels(t *testing.T) {
	got := EstimatedModels([]string{
		"claude-opus-5",  // exact - excluded
		"claude-opus-6",  // family fallback
		"claude-opus-6",  // duplicate of the above
		"gpt-4",          // global fallback
		"claude-haiku-6", // family fallback
	})

	want := []string{"claude-haiku-6", "claude-opus-6", "gpt-4"}
	if len(got) != len(want) {
		t.Fatalf("EstimatedModels() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("EstimatedModels()[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	if EstimatedModels([]string{"claude-opus-5", "claude-sonnet-4-6"}) != nil {
		t.Error("EstimatedModels() must be empty when every model is priced exactly")
	}
}
