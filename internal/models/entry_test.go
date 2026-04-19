package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormaliseModelName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Opus 4.6
		{"claude-opus-4-6", "claude-opus-4-6"},
		{"claude-opus-4-6-20260101", "claude-opus-4-6"},
		{"Claude-Opus-4-6", "claude-opus-4-6"},
		{"claude-opus-4.6", "claude-opus-4-6"},

		// Opus 4.5
		{"claude-opus-4-5-20251101", "claude-opus-4-5"},
		{"claude-opus-4-5", "claude-opus-4-5"},
		{"claude-opus-4.5", "claude-opus-4-5"},

		// Opus 4.1
		{"claude-opus-4-1-20250805", "claude-opus-4-1"},
		{"claude-opus-4-1", "claude-opus-4-1"},
		{"claude-opus-4.1", "claude-opus-4-1"},

		// Opus 4 (base)
		{"claude-opus-4-20250514", "claude-opus-4"},
		{"opus", "claude-opus-4"},

		// Opus 3
		{"claude-3-opus-20240229", "claude-3-opus"},

		// Sonnet 4.6
		{"claude-sonnet-4-6", "claude-sonnet-4-6"},
		{"claude-sonnet-4-6-20260101", "claude-sonnet-4-6"},
		{"Claude-Sonnet-4.6", "claude-sonnet-4-6"},

		// Sonnet 4.5
		{"claude-sonnet-4-5-20250929", "claude-sonnet-4-5"},
		{"claude-sonnet-4-5", "claude-sonnet-4-5"},

		// Sonnet 4
		{"claude-sonnet-4-20250514", "claude-sonnet-4"},
		{"claude-sonnet-4", "claude-sonnet-4"},

		// Sonnet 3.5
		{"claude-3-5-sonnet-20241022", "claude-3-5-sonnet"},
		{"claude-3.5-sonnet", "claude-3-5-sonnet"},

		// Sonnet 3
		{"sonnet", "claude-3-sonnet"},

		// Haiku 4.5
		{"claude-haiku-4-5-20251001", "claude-haiku-4-5"},
		{"claude-haiku-4-5", "claude-haiku-4-5"},
		{"claude-haiku-4.5", "claude-haiku-4-5"},

		// Haiku 3.5
		{"claude-3-5-haiku-20241022", "claude-3-5-haiku"},

		// Haiku 3
		{"haiku", "claude-3-haiku"},

		// Unknown passthrough
		{"gpt-4", "gpt-4"},
		{"<synthetic>", "<synthetic>"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, NormaliseModelName(tt.input))
		})
	}
}

// TestDisplayTokensVsTotalTokens pins down the parity contract with the Python
// implementation (see CLAUDE.md): DisplayTokens is input+output only and is what
// the UI shows; TotalTokens adds cache-creation and cache-read tokens and is what
// the cost calculator reads. If either method ever changes semantics, these
// tests are the tripwire.
func TestDisplayTokensVsTotalTokens(t *testing.T) {
	tests := []struct {
		name             string
		entry            UsageEntry
		wantDisplay      int
		wantTotal        int
	}{
		{
			name: "all token types present",
			entry: UsageEntry{
				InputTokens:         100,
				OutputTokens:        50,
				CacheCreationTokens: 1000,
				CacheReadTokens:     5000,
			},
			wantDisplay: 150,
			wantTotal:   6150,
		},
		{
			name: "no cache tokens",
			entry: UsageEntry{
				InputTokens:  200,
				OutputTokens: 300,
			},
			wantDisplay: 500,
			wantTotal:   500,
		},
		{
			name: "cache-only entry",
			entry: UsageEntry{
				CacheCreationTokens: 400,
				CacheReadTokens:     100,
			},
			wantDisplay: 0,
			wantTotal:   500,
		},
		{
			name:        "zero entry",
			entry:       UsageEntry{},
			wantDisplay: 0,
			wantTotal:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantDisplay, tt.entry.DisplayTokens(),
				"DisplayTokens must exclude cache tokens")
			assert.Equal(t, tt.wantTotal, tt.entry.TotalTokens(),
				"TotalTokens must include every token type")
		})
	}
}

// TestModelStatsDisplayTokensVsTotalTokens mirrors the parity check for the
// aggregate ModelStats type, which has the same invariant.
func TestModelStatsDisplayTokensVsTotalTokens(t *testing.T) {
	stats := ModelStats{
		InputTokens:         1000,
		OutputTokens:        2000,
		CacheCreationTokens: 3000,
		CacheReadTokens:     4000,
	}
	assert.Equal(t, 3000, stats.DisplayTokens(), "DisplayTokens must be input+output")
	assert.Equal(t, 10000, stats.TotalTokens(), "TotalTokens must sum every field")
}
