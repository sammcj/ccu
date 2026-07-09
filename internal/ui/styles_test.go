package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetModelColour(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  string
	}{
		{name: "fable", model: "Fable", want: string(ColorFable)},
		{name: "mythos", model: "Mythos", want: string(ColorMythos)},
		{name: "opus", model: "claude-opus-4-20250514", want: string(ColorOpus)},
		{name: "sonnet", model: "claude-sonnet-4", want: string(ColorSonnet)},
		{name: "haiku", model: "Haiku 4.5", want: string(ColorHaiku)},
		{name: "case insensitive", model: "FABLE", want: string(ColorFable)},
		{name: "unknown model falls back", model: "Nimbus", want: string(ColorModelUnknown)},
		{name: "empty falls back", model: "", want: string(ColorModelUnknown)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, string(GetModelColour(tt.model)))
		})
	}
}

// Fable must not collide with Opus's mellow pink, or the two weekly bars
// become indistinguishable.
func TestModelColoursAreDistinct(t *testing.T) {
	colours := map[string]string{
		"sonnet":  string(ColorSonnet),
		"opus":    string(ColorOpus),
		"haiku":   string(ColorHaiku),
		"fable":   string(ColorFable),
		"mythos":  string(ColorMythos),
		"unknown": string(ColorModelUnknown),
	}

	seen := make(map[string]string, len(colours))
	for model, colour := range colours {
		if other, dup := seen[colour]; dup {
			t.Errorf("models %q and %q share colour %s", model, other, colour)
		}
		seen[colour] = model
	}
}
