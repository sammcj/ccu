package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/sammcj/ccu/internal/models"
)

// periodStats holds statistics aggregated for a single time period (day or month).
type periodStats struct {
	Period       time.Time
	TotalTokens  int
	TotalCost    float64
	MessageCount int
	Models       map[string]bool
}

// RenderDailyView renders the daily aggregation view.
func RenderDailyView(entries []models.UsageEntry, width int) string {
	return renderPeriodView(entries, "2006-01-02", "📅 Daily Usage Report",
		"Date", 12, "No data available for daily view")
}

// RenderMonthlyView renders the monthly aggregation view.
func RenderMonthlyView(entries []models.UsageEntry, width int) string {
	return renderPeriodView(entries, "2006-01", "📊 Monthly Usage Report",
		"Month", 10, "No data available for monthly view")
}

// renderPeriodView renders an aggregation table grouped by the given period format.
func renderPeriodView(entries []models.UsageEntry, periodFormat, title, periodLabel string, periodWidth int, emptyMsg string) string {
	if len(entries) == 0 {
		return WarningStyle.Render(emptyMsg + "\n\nPress q to exit")
	}

	stats := aggregatePeriod(entries, periodFormat)
	separator := strings.Repeat("─", periodWidth+57)

	var lines []string
	lines = append(lines, TitleStyle.Render(title))
	lines = append(lines, "")

	header := fmt.Sprintf("%-*s  %-15s  %-12s  %-10s  %-10s",
		periodWidth, periodLabel, "Models", "Tokens", "Cost", "Messages")
	lines = append(lines, HeaderStyle.Render(header))
	lines = append(lines, LabelStyle.Render(separator))

	totalTokens := 0
	totalCost := 0.0
	totalMessages := 0

	for _, s := range stats {
		row := fmt.Sprintf("%-*s  %-15s  %12s  %10s  %10s",
			periodWidth,
			s.Period.Format(periodFormat),
			formatModels(s.Models),
			formatNumber(s.TotalTokens),
			fmt.Sprintf("$%.2f", s.TotalCost),
			fmt.Sprintf("%d", s.MessageCount))
		lines = append(lines, row)

		totalTokens += s.TotalTokens
		totalCost += s.TotalCost
		totalMessages += s.MessageCount
	}

	lines = append(lines, LabelStyle.Render(separator))
	totalRow := fmt.Sprintf("%-*s  %-15s  %12s  %10s  %10s",
		periodWidth, "Total", "", formatNumber(totalTokens), fmt.Sprintf("$%.2f", totalCost), fmt.Sprintf("%d", totalMessages))
	lines = append(lines, ValueStyle.Render(totalRow))

	lines = append(lines, "")
	lines = append(lines, HelpStyle.Render("Press q to exit"))

	return strings.Join(lines, "\n")
}

// aggregatePeriod groups entries into periods keyed by the given time format,
// returning stats sorted chronologically. Times are treated as UTC.
func aggregatePeriod(entries []models.UsageEntry, periodFormat string) []periodStats {
	statsMap := make(map[string]*periodStats)

	for _, entry := range entries {
		key := entry.Timestamp.Format(periodFormat)

		if statsMap[key] == nil {
			// Parse the key back with the same layout to get the period start.
			periodTime, _ := time.Parse(periodFormat, key)
			statsMap[key] = &periodStats{
				Period: periodTime,
				Models: make(map[string]bool),
			}
		}

		stats := statsMap[key]
		stats.TotalTokens += entry.DisplayTokens() // Match Python UI - only input + output
		stats.TotalCost += entry.CostUSD
		stats.MessageCount++
		stats.Models[models.NormaliseModelName(entry.Model)] = true
	}

	result := make([]periodStats, 0, len(statsMap))
	for _, stats := range statsMap {
		result = append(result, *stats)
	}

	slices.SortFunc(result, func(a, b periodStats) int {
		return a.Period.Compare(b.Period)
	})

	return result
}

// formatModels formats a set of normalised model names for table display,
// collapsing each name to its family. Output is sorted for deterministic display.
func formatModels(models map[string]bool) string {
	if len(models) == 0 {
		return "-"
	}

	families := make(map[string]bool)
	for model := range models {
		if family := modelFamily(model); family != "" {
			families[family] = true
		}
	}

	if len(families) == 0 {
		return "Mixed"
	}

	names := make([]string, 0, len(families))
	for family := range families {
		names = append(names, family)
	}
	slices.Sort(names)

	return strings.Join(names, "/")
}

// modelFamily maps a normalised model name to its display family, matching the
// substring approach used by GetModelColour. Returns "" for unrecognised names.
func modelFamily(model string) string {
	lower := strings.ToLower(model)
	switch {
	case strings.Contains(lower, "fable"):
		return "Fable"
	case strings.Contains(lower, "mythos"):
		return "Mythos"
	case strings.Contains(lower, "opus"):
		return "Opus"
	case strings.Contains(lower, "sonnet"):
		return "Sonnet"
	case strings.Contains(lower, "haiku"):
		return "Haiku"
	default:
		return ""
	}
}

// formatNumber formats large numbers with thousands separators.
func formatNumber(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}

	var result strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result.WriteRune(',')
		}
		result.WriteRune(c)
	}

	return result.String()
}
