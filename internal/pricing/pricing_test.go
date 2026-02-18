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
