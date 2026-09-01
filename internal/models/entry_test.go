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
		// Fable 5 / Mythos 5
		{"claude-fable-5", "claude-fable-5"},
		{"Claude-Fable-5", "claude-fable-5"},
		{"claude-mythos-5", "claude-mythos-5"},

		// Opus 4.8
		{"claude-opus-4-8", "claude-opus-4-8"},
		{"claude-opus-4.8", "claude-opus-4-8"},

		// Opus 4.7
		{"claude-opus-4-7", "claude-opus-4-7"},
		{"claude-opus-4.7", "claude-opus-4-7"},

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

		// Opus 5
		{"claude-opus-5", "claude-opus-5"},
		{"claude-opus-5-20260501", "claude-opus-5"},
		// The [1m] context-window suffix must not leak into the key
		{"claude-opus-5[1m]", "claude-opus-5"},

		// Opus 4 (base). The trailing date stamp must not be read as a minor
		// version - "claude-opus-4-20250514" is Opus 4, not Opus 4.20.
		{"claude-opus-4-20250514", "claude-opus-4"},

		// Opus 3
		{"claude-3-opus-20240229", "claude-3-opus"},

		// Version-before-family IDs. Anthropic still publishes these for some
		// Claude 4 models, so they are not a Claude 3 relic. A version of 4 or
		// above keys the modern way round even when the ID does not.
		{"claude-4-opus-20250514", "claude-opus-4"},
		{"claude-4-sonnet-20250514", "claude-sonnet-4"},
		{"claude-4-5-sonnet", "claude-sonnet-4-5"},

		// Sonnet 5
		{"claude-sonnet-5", "claude-sonnet-5"},

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

		// Sonnet 3.5 / 3 (legacy version-before-family naming)
		{"claude-3-5-sonnet-20241022", "claude-3-5-sonnet"},
		{"claude-3.5-sonnet", "claude-3-5-sonnet"},
		{"claude-3-sonnet-20240229", "claude-3-sonnet"},

		// Haiku 4.5
		{"claude-haiku-4-5-20251001", "claude-haiku-4-5"},
		{"claude-haiku-4-5", "claude-haiku-4-5"},
		{"claude-haiku-4.5", "claude-haiku-4-5"},

		// Haiku 3.5 / 3
		{"claude-3-5-haiku-20241022", "claude-3-5-haiku"},
		{"claude-3-haiku-20240307", "claude-3-haiku"},

		// Unversioned family names resolve to that family's newest release.
		// Claude Code writes these into some JSONL entries; resolving them to an
		// older generation would price current usage at a retired model's rate.
		{"opus", "claude-opus-5"},
		{"sonnet", "claude-sonnet-5"},
		{"haiku", "claude-haiku-4-5"},
		{"fable", "claude-fable-5-1"},
		{"mythos", "claude-mythos-5-1"},

		// Unknown passthrough
		{"gpt-4", "gpt-4"},
		{"<synthetic>", "<synthetic>"},
		{"gemma-4-26b-a4b-it-5bit", "gemma-4-26b-a4b-it-5bit"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, NormaliseModelName(tt.input))
		})
	}
}

// TestNormaliseUnreleasedModelVersions is the regression test for the Opus 5
// bug: normalisation used to enumerate known versions and fall back to the
// family's OLDEST generation, so "claude-opus-5" silently became
// "claude-opus-4" and was costed at a retired model's rate.
//
// These model IDs do not exist. That is the point - they stand in for whatever
// Anthropic ships next, and they fail if anyone reintroduces an enumerated
// version list. Do not "fix" this test by adding these versions to the code.
func TestNormaliseUnreleasedModelVersions(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"claude-opus-6", "claude-opus-6"},
		{"claude-opus-6-1", "claude-opus-6-1"},
		{"claude-opus-6-1-20270101", "claude-opus-6-1"},
		{"claude-sonnet-6", "claude-sonnet-6"},
		{"claude-haiku-6", "claude-haiku-6"},
		{"claude-fable-6", "claude-fable-6"},
		{"claude-opus-10", "claude-opus-10"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, NormaliseModelName(tt.input),
				"an unreleased version must normalise to its own key, never to an older generation")
		})
	}
}

// TestNormaliseModelNameIsIdempotent guards a property the pricing lookup and
// the dashboard's estimated-pricing notice both rely on: they re-normalise
// names that are already canonical.
func TestNormaliseModelNameIsIdempotent(t *testing.T) {
	inputs := []string{
		"claude-opus-5", "claude-opus-4-8", "claude-opus-4", "claude-3-opus",
		"claude-sonnet-5", "claude-3-5-sonnet", "claude-haiku-4-5",
		"claude-fable-5", "claude-mythos-5", "gpt-4", "<synthetic>",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			once := NormaliseModelName(input)
			assert.Equal(t, once, NormaliseModelName(once),
				"normalising an already-canonical name must be a no-op")
		})
	}
}

// TestEveryFamilyHasALatestVersion stops a new entry in ModelFamilies from
// silently producing "claude-<family>-" for an unversioned model name.
func TestEveryFamilyHasALatestVersion(t *testing.T) {
	for _, family := range ModelFamilies {
		assert.NotEmpty(t, latestFamilyVersion[family],
			"family %q needs an entry in latestFamilyVersion", family)
	}
}

// TestDisplayTokensVsTotalTokens pins down the parity contract with the Python
// implementation (see CLAUDE.md): DisplayTokens is input+output only and is what
// the UI shows; TotalTokens adds cache-creation and cache-read tokens and is what
// the cost calculator reads. If either method ever changes semantics, these
// tests are the tripwire.
func TestDisplayTokensVsTotalTokens(t *testing.T) {
	tests := []struct {
		name        string
		entry       UsageEntry
		wantDisplay int
		wantTotal   int
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

// TestUsageEntryHash asserts the dedupe key semantics: distinct ID pairs give
// distinct keys, identical pairs collide.
func TestUsageEntryHash(t *testing.T) {
	a := UsageEntry{MessageID: "msg_1", RequestID: "req_1"}
	b := UsageEntry{MessageID: "msg_1", RequestID: "req_1"}
	c := UsageEntry{MessageID: "msg_1", RequestID: "req_2"}

	assert.Equal(t, "msg_1:req_1", a.Hash())
	assert.Equal(t, a.Hash(), b.Hash())
	assert.NotEqual(t, a.Hash(), c.Hash())
}
