package ui

import (
	"testing"
	"time"

	"github.com/sammcj/ccu/internal/models"
	"github.com/stretchr/testify/assert"
)

// Helper to create a UsageEntry with specific values
func makeEntry(ts time.Time, model string, input, output, cacheCreate, cacheRead int, cost float64) models.UsageEntry {
	return models.UsageEntry{
		Timestamp:           ts,
		Model:               model,
		InputTokens:         input,
		OutputTokens:        output,
		CacheCreationTokens: cacheCreate,
		CacheReadTokens:     cacheRead,
		CostUSD:             cost,
	}
}

func TestAggregateForReport_Daily(t *testing.T) {
	tz := time.UTC

	// Create entries across 3 different days
	day1 := time.Date(2025, 12, 15, 10, 30, 0, 0, tz)
	day2 := time.Date(2025, 12, 16, 14, 0, 0, 0, tz)
	day3 := time.Date(2025, 12, 17, 9, 15, 0, 0, tz)

	entries := []models.UsageEntry{
		makeEntry(day1, "claude-sonnet-4", 100, 50, 200, 300, 0.01),
		makeEntry(day1.Add(2*time.Hour), "claude-sonnet-4", 150, 75, 100, 200, 0.015),
		makeEntry(day2, "claude-sonnet-4", 200, 100, 300, 400, 0.02),
		makeEntry(day3, "claude-sonnet-4", 50, 25, 50, 100, 0.005),
	}

	stats := aggregateForReport(entries, "daily", tz)

	assert.Len(t, stats, 3, "should have 3 daily periods")

	// Verify day 1 aggregation (2 entries)
	assert.Equal(t, "2025-12-15", stats[0].Period.Format("2006-01-02"))
	day1Stats := stats[0].ModelStats["claude-sonnet-4"]
	assert.Equal(t, 250, day1Stats.InputTokens, "day 1 input: 100 + 150")
	assert.Equal(t, 125, day1Stats.OutputTokens, "day 1 output: 50 + 75")
	assert.Equal(t, 300, day1Stats.CacheCreationTokens, "day 1 cache create: 200 + 100")
	assert.Equal(t, 500, day1Stats.CacheReadTokens, "day 1 cache read: 300 + 200")
	assert.Equal(t, 1175, day1Stats.TotalTokens, "day 1 total: 250+125+300+500")
	assert.InDelta(t, 0.025, day1Stats.TotalCost, 0.0001, "day 1 cost: 0.01 + 0.015")

	// Verify day 2 aggregation (1 entry)
	assert.Equal(t, "2025-12-16", stats[1].Period.Format("2006-01-02"))
	day2Stats := stats[1].ModelStats["claude-sonnet-4"]
	assert.Equal(t, 200, day2Stats.InputTokens)
	assert.Equal(t, 100, day2Stats.OutputTokens)
	assert.Equal(t, 1000, day2Stats.TotalTokens, "day 2 total: 200+100+300+400")

	// Verify day 3 aggregation (1 entry)
	assert.Equal(t, "2025-12-17", stats[2].Period.Format("2006-01-02"))
	day3Stats := stats[2].ModelStats["claude-sonnet-4"]
	assert.Equal(t, 225, day3Stats.TotalTokens, "day 3 total: 50+25+50+100")
}

func TestAggregateForReport_Weekly(t *testing.T) {
	tz := time.UTC

	// Create entries across 2 different ISO weeks
	// Week 50 of 2025: Dec 8-14
	// Week 51 of 2025: Dec 15-21
	week50Entry := time.Date(2025, 12, 10, 10, 0, 0, 0, tz) // Wednesday of week 50
	week51Entry := time.Date(2025, 12, 17, 14, 0, 0, 0, tz) // Wednesday of week 51

	entries := []models.UsageEntry{
		makeEntry(week50Entry, "claude-sonnet-4", 100, 50, 200, 300, 0.01),
		makeEntry(week51Entry, "claude-sonnet-4", 200, 100, 300, 400, 0.02),
	}

	stats := aggregateForReport(entries, "weekly", tz)

	assert.Len(t, stats, 2, "should have 2 weekly periods")

	// Verify week 50
	year1, week1 := stats[0].Period.ISOWeek()
	assert.Equal(t, 2025, year1)
	assert.Equal(t, 50, week1)
	assert.Equal(t, 650, stats[0].ModelStats["claude-sonnet-4"].TotalTokens)

	// Verify week 51
	year2, week2 := stats[1].Period.ISOWeek()
	assert.Equal(t, 2025, year2)
	assert.Equal(t, 51, week2)
	assert.Equal(t, 1000, stats[1].ModelStats["claude-sonnet-4"].TotalTokens)
}

func TestAggregateForReport_Monthly(t *testing.T) {
	tz := time.UTC

	// Create entries across 2 different months
	nov := time.Date(2025, 11, 15, 10, 0, 0, 0, tz)
	dec := time.Date(2025, 12, 10, 14, 0, 0, 0, tz)

	entries := []models.UsageEntry{
		makeEntry(nov, "claude-sonnet-4", 100, 50, 200, 300, 0.01),
		makeEntry(nov.Add(5*24*time.Hour), "claude-sonnet-4", 50, 25, 100, 150, 0.005),
		makeEntry(dec, "claude-sonnet-4", 200, 100, 300, 400, 0.02),
	}

	stats := aggregateForReport(entries, "monthly", tz)

	assert.Len(t, stats, 2, "should have 2 monthly periods")

	// Verify November (2 entries)
	assert.Equal(t, "2025-11", stats[0].Period.Format("2006-01"))
	novStats := stats[0].ModelStats["claude-sonnet-4"]
	assert.Equal(t, 150, novStats.InputTokens, "Nov input: 100 + 50")
	assert.Equal(t, 75, novStats.OutputTokens, "Nov output: 50 + 25")
	assert.Equal(t, 975, novStats.TotalTokens, "Nov total: 150+75+300+450")

	// Verify December (1 entry)
	assert.Equal(t, "2025-12", stats[1].Period.Format("2006-01"))
	decStats := stats[1].ModelStats["claude-sonnet-4"]
	assert.Equal(t, 1000, decStats.TotalTokens, "Dec total: 200+100+300+400")
}

func TestAggregateForReport_MultipleModels(t *testing.T) {
	tz := time.UTC
	day := time.Date(2025, 12, 15, 10, 0, 0, 0, tz)

	entries := []models.UsageEntry{
		makeEntry(day, "claude-sonnet-4", 100, 50, 200, 300, 0.01),
		makeEntry(day.Add(1*time.Hour), "claude-opus-4-5", 500, 250, 1000, 2000, 0.10),
		makeEntry(day.Add(2*time.Hour), "claude-haiku-4-5", 50, 25, 100, 150, 0.002),
	}

	stats := aggregateForReport(entries, "daily", tz)

	assert.Len(t, stats, 1, "should have 1 daily period")
	assert.Len(t, stats[0].ModelStats, 3, "should have 3 models in period")

	// Verify each model
	sonnetStats := stats[0].ModelStats["claude-sonnet-4"]
	assert.Equal(t, 650, sonnetStats.TotalTokens, "sonnet total: 100+50+200+300")

	opusStats := stats[0].ModelStats["claude-opus-4-5"]
	assert.Equal(t, 3750, opusStats.TotalTokens, "opus total: 500+250+1000+2000")

	haikuStats := stats[0].ModelStats["claude-haiku-4-5"]
	assert.Equal(t, 325, haikuStats.TotalTokens, "haiku total: 50+25+100+150")

	// Verify period total sums all models
	assert.Equal(t, 4725, stats[0].TotalTokens, "period total: 650+3750+325")
}

func TestAggregateForReport_TotalTokenCalculation(t *testing.T) {
	tz := time.UTC
	ts := time.Date(2025, 12, 15, 10, 0, 0, 0, tz)

	// Known values for verification
	input := 100
	output := 50
	cacheCreate := 200
	cacheRead := 300
	expectedTotal := input + output + cacheCreate + cacheRead // 650

	entries := []models.UsageEntry{
		makeEntry(ts, "claude-sonnet-4", input, output, cacheCreate, cacheRead, 0.01),
	}

	stats := aggregateForReport(entries, "daily", tz)

	assert.Len(t, stats, 1)
	ms := stats[0].ModelStats["claude-sonnet-4"]

	assert.Equal(t, input, ms.InputTokens)
	assert.Equal(t, output, ms.OutputTokens)
	assert.Equal(t, cacheCreate, ms.CacheCreationTokens)
	assert.Equal(t, cacheRead, ms.CacheReadTokens)
	assert.Equal(t, expectedTotal, ms.TotalTokens, "TotalTokens should be sum of all 4 types")
}

func TestAggregateForReport_EmptyEntries(t *testing.T) {
	tz := time.UTC
	entries := []models.UsageEntry{}

	stats := aggregateForReport(entries, "daily", tz)

	assert.Len(t, stats, 0, "empty entries should return empty stats")
}

func TestGetWeekStart(t *testing.T) {
	tz := time.UTC

	tests := []struct {
		name     string
		input    time.Time
		expected time.Time
	}{
		{
			name:     "Monday returns same day",
			input:    time.Date(2025, 12, 15, 10, 30, 0, 0, tz), // Monday
			expected: time.Date(2025, 12, 15, 0, 0, 0, 0, tz),
		},
		{
			name:     "Wednesday returns Monday",
			input:    time.Date(2025, 12, 17, 14, 0, 0, 0, tz), // Wednesday
			expected: time.Date(2025, 12, 15, 0, 0, 0, 0, tz),  // Monday
		},
		{
			name:     "Sunday returns previous Monday",
			input:    time.Date(2025, 12, 21, 23, 59, 0, 0, tz), // Sunday
			expected: time.Date(2025, 12, 15, 0, 0, 0, 0, tz),   // Monday
		},
		{
			name:     "Saturday returns Monday",
			input:    time.Date(2025, 12, 20, 12, 0, 0, 0, tz), // Saturday
			expected: time.Date(2025, 12, 15, 0, 0, 0, 0, tz),  // Monday
		},
		{
			name:     "Tuesday returns Monday",
			input:    time.Date(2025, 12, 16, 8, 0, 0, 0, tz), // Tuesday
			expected: time.Date(2025, 12, 15, 0, 0, 0, 0, tz), // Monday
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getWeekStart(tt.input, tz)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetSortedModelNames(t *testing.T) {
	modelStats := map[string]*ModelStats{
		"claude-opus-4-5":   {},
		"claude-haiku-4-5":  {},
		"claude-sonnet-4":   {},
		"claude-3-5-sonnet": {},
	}

	sorted := getSortedModelNames(modelStats)

	expected := []string{
		"claude-3-5-sonnet",
		"claude-haiku-4-5",
		"claude-opus-4-5",
		"claude-sonnet-4",
	}
	assert.Equal(t, expected, sorted, "model names should be sorted alphabetically")
}

func TestRenderReport_GrandTotals(t *testing.T) {
	tz := time.UTC

	// Create entries with known totals
	day1 := time.Date(2025, 12, 15, 10, 0, 0, 0, tz)
	day2 := time.Date(2025, 12, 16, 10, 0, 0, 0, tz)

	entries := []models.UsageEntry{
		makeEntry(day1, "claude-sonnet-4", 100, 50, 200, 300, 1.50),
		makeEntry(day1, "claude-opus-4-5", 500, 250, 1000, 2000, 5.00),
		makeEntry(day2, "claude-sonnet-4", 200, 100, 300, 400, 2.00),
	}

	// Generate report
	report := GenerateDailyReport(entries, tz)

	// Verify grand totals appear in output
	// Day 1 Sonnet: 650 tokens, $1.50
	// Day 1 Opus: 3750 tokens, $5.00
	// Day 2 Sonnet: 1000 tokens, $2.00
	// Grand Total: 5400 tokens, $8.50
	assert.Contains(t, report, "TOTAL", "report should contain TOTAL row")
	assert.Contains(t, report, "$8.50", "report should show total cost of $8.50")

	// Verify individual columns are present
	assert.Contains(t, report, "800", "total input: 100+500+200 = 800")
	assert.Contains(t, report, "400", "total output: 50+250+100 = 400")
}

func TestAggregateForReport_SortsChronologically(t *testing.T) {
	tz := time.UTC

	// Add entries out of order
	dec := time.Date(2025, 12, 15, 10, 0, 0, 0, tz)
	nov := time.Date(2025, 11, 10, 10, 0, 0, 0, tz)
	oct := time.Date(2025, 10, 5, 10, 0, 0, 0, tz)

	entries := []models.UsageEntry{
		makeEntry(dec, "claude-sonnet-4", 100, 50, 200, 300, 0.01),
		makeEntry(oct, "claude-sonnet-4", 100, 50, 200, 300, 0.01),
		makeEntry(nov, "claude-sonnet-4", 100, 50, 200, 300, 0.01),
	}

	stats := aggregateForReport(entries, "monthly", tz)

	assert.Len(t, stats, 3)
	// Should be sorted chronologically: Oct, Nov, Dec
	assert.Equal(t, "2025-10", stats[0].Period.Format("2006-01"))
	assert.Equal(t, "2025-11", stats[1].Period.Format("2006-01"))
	assert.Equal(t, "2025-12", stats[2].Period.Format("2006-01"))
}
