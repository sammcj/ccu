package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWeeklyHoursForModel(t *testing.T) {
	tests := []struct {
		name        string
		plan        string
		displayName string
		want        float64
	}{
		{name: "sonnet on max5", plan: "max5", displayName: "Sonnet", want: 210},
		{name: "sonnet on max20", plan: "max20", displayName: "Sonnet", want: 360},
		{name: "sonnet on pro", plan: "pro", displayName: "Sonnet", want: 60},
		{name: "display name is matched case insensitively", plan: "max5", displayName: "sonnet 4.5", want: 210},
		{name: "opus has no published allowance", plan: "max5", displayName: "Opus", want: 0},
		{name: "fable has no published allowance", plan: "max5", displayName: "Fable", want: 0},
		{name: "unknown model has no allowance", plan: "max5", displayName: "Mythos", want: 0},
		{name: "unknown plan falls back to pro", plan: "enterprise", displayName: "Sonnet", want: 60},
		{name: "empty display name", plan: "max5", displayName: "", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, WeeklyHoursForModel(tt.plan, tt.displayName))
		})
	}
}
