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
