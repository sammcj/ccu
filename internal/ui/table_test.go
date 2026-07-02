package ui

import (
	"testing"
	"time"

	"github.com/sammcj/ccu/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestFormatModels(t *testing.T) {
	tests := []struct {
		name   string
		models map[string]bool
		want   string
	}{
		{"empty", map[string]bool{}, "-"},
		{"opus current", map[string]bool{"claude-opus-4-8": true}, "Opus"},
		{"sonnet current", map[string]bool{"claude-sonnet-4-6": true}, "Sonnet"},
		{"haiku current", map[string]bool{"claude-haiku-4-5": true}, "Haiku"},
		{"fable", map[string]bool{"claude-fable-5": true}, "Fable"},
		{"mythos", map[string]bool{"claude-mythos-5": true}, "Mythos"},
		{"legacy opus", map[string]bool{"claude-3-opus": true}, "Opus"},
		{
			name:   "multiple sorted",
			models: map[string]bool{"claude-sonnet-4-6": true, "claude-opus-4-8": true},
			want:   "Opus/Sonnet",
		},
		{
			name:   "duplicate family collapses",
			models: map[string]bool{"claude-opus-4-8": true, "claude-opus-4-5": true},
			want:   "Opus",
		},
		{"unknown only", map[string]bool{"gpt-4": true}, "Mixed"},
		{
			name:   "known plus unknown keeps known",
			models: map[string]bool{"claude-opus-4-8": true, "gpt-4": true},
			want:   "Opus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, formatModels(tt.models))
		})
	}
}

func TestAggregatePeriod_Daily(t *testing.T) {
	tz := time.UTC
	day1 := time.Date(2025, 12, 15, 10, 30, 0, 0, tz)
	day2 := time.Date(2025, 12, 16, 14, 0, 0, 0, tz)

	entries := []models.UsageEntry{
		makeEntry(day1, "claude-sonnet-4-6", 100, 50, 200, 300, 0.01),
		makeEntry(day1.Add(2*time.Hour), "claude-sonnet-4-6", 150, 75, 100, 200, 0.015),
		makeEntry(day2, "claude-opus-4-8", 200, 100, 300, 400, 0.02),
	}

	stats := aggregatePeriod(entries, "2006-01-02")

	assert.Len(t, stats, 2, "should have 2 daily periods")

	// Day 1: 2 entries, DisplayTokens = input + output only
	assert.Equal(t, "2025-12-15", stats[0].Period.Format("2006-01-02"))
	assert.Equal(t, 375, stats[0].TotalTokens, "day 1 display tokens: 100+50+150+75")
	assert.Equal(t, 2, stats[0].MessageCount)
	assert.InDelta(t, 0.025, stats[0].TotalCost, 0.0001)

	// Day 2: 1 entry
	assert.Equal(t, "2025-12-16", stats[1].Period.Format("2006-01-02"))
	assert.Equal(t, 300, stats[1].TotalTokens, "day 2 display tokens: 200+100")
	assert.Equal(t, 1, stats[1].MessageCount)
}

func TestAggregatePeriod_Monthly(t *testing.T) {
	tz := time.UTC
	nov := time.Date(2025, 11, 15, 10, 0, 0, 0, tz)
	dec := time.Date(2025, 12, 10, 14, 0, 0, 0, tz)

	entries := []models.UsageEntry{
		makeEntry(nov, "claude-sonnet-4-6", 100, 50, 200, 300, 0.01),
		makeEntry(nov.Add(5*24*time.Hour), "claude-sonnet-4-6", 50, 25, 100, 150, 0.005),
		makeEntry(dec, "claude-sonnet-4-6", 200, 100, 300, 400, 0.02),
	}

	stats := aggregatePeriod(entries, "2006-01")

	assert.Len(t, stats, 2, "should have 2 monthly periods")
	assert.Equal(t, "2025-11", stats[0].Period.Format("2006-01"))
	assert.Equal(t, 225, stats[0].TotalTokens, "Nov display tokens: 100+50+50+25")
	assert.Equal(t, "2025-12", stats[1].Period.Format("2006-01"))
	assert.Equal(t, 300, stats[1].TotalTokens, "Dec display tokens: 200+100")
}

func TestAggregatePeriod_SortsChronologically(t *testing.T) {
	tz := time.UTC
	dec := time.Date(2025, 12, 15, 10, 0, 0, 0, tz)
	nov := time.Date(2025, 11, 10, 10, 0, 0, 0, tz)
	oct := time.Date(2025, 10, 5, 10, 0, 0, 0, tz)

	entries := []models.UsageEntry{
		makeEntry(dec, "claude-sonnet-4-6", 100, 50, 200, 300, 0.01),
		makeEntry(oct, "claude-sonnet-4-6", 100, 50, 200, 300, 0.01),
		makeEntry(nov, "claude-sonnet-4-6", 100, 50, 200, 300, 0.01),
	}

	stats := aggregatePeriod(entries, "2006-01")

	assert.Len(t, stats, 3)
	assert.Equal(t, "2025-10", stats[0].Period.Format("2006-01"))
	assert.Equal(t, "2025-11", stats[1].Period.Format("2006-01"))
	assert.Equal(t, "2025-12", stats[2].Period.Format("2006-01"))
}

func TestRenderDailyView(t *testing.T) {
	tz := time.UTC
	day := time.Date(2025, 12, 15, 10, 0, 0, 0, tz)

	entries := []models.UsageEntry{
		makeEntry(day, "claude-opus-4-8", 100, 50, 200, 300, 1.50),
	}

	out := RenderDailyView(entries, 80)

	assert.Contains(t, out, "Daily Usage Report")
	assert.Contains(t, out, "2025-12-15")
	assert.Contains(t, out, "Opus")
	assert.Contains(t, out, "Total")
}

func TestRenderMonthlyView(t *testing.T) {
	tz := time.UTC
	month := time.Date(2025, 12, 15, 10, 0, 0, 0, tz)

	entries := []models.UsageEntry{
		makeEntry(month, "claude-sonnet-4-6", 100, 50, 200, 300, 1.50),
	}

	out := RenderMonthlyView(entries, 80)

	assert.Contains(t, out, "Monthly Usage Report")
	assert.Contains(t, out, "2025-12")
	assert.Contains(t, out, "Sonnet")
	assert.Contains(t, out, "Total")
}

func TestRenderDailyView_Empty(t *testing.T) {
	out := RenderDailyView(nil, 80)
	assert.Contains(t, out, "No data available")
}
