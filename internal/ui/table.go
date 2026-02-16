package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/sammcj/ccu/internal/models"
)

// DailyStats holds daily aggregated statistics
type DailyStats struct {
	Date         time.Time
	TotalTokens  int
	TotalCost    float64
	MessageCount int
	Models       map[string]bool
}

// MonthlyStats holds monthly aggregated statistics
type MonthlyStats struct {
	Month        time.Time
	TotalTokens  int
	TotalCost    float64
	MessageCount int
	Models       map[string]bool
}

// RenderDailyView renders the daily aggregation view
func RenderDailyView(entries []models.UsageEntry, width int) string {
	if len(entries) == 0 {
		return WarningStyle.Render("No data available for daily view\n\nPress q to exit")
	}

	// Aggregate by day
	dailyStats := aggregateByDay(entries)

	var lines []string
	lines = append(lines, TitleStyle.Render("📅 Daily Usage Report"))
	lines = append(lines, "")

	// Header
	header := fmt.Sprintf("%-12s  %-15s  %-12s  %-10s  %-10s",
		"Date", "Models", "Tokens", "Cost", "Messages")
	lines = append(lines, HeaderStyle.Render(header))
	lines = append(lines, LabelStyle.Render("─────────────────────────────────────────────────────────────────────"))

	// Rows
	totalTokens := 0
	totalCost := 0.0
	totalMessages := 0

	for _, stats := range dailyStats {
		dateStr := stats.Date.Format("2006-01-02")
		modelsStr := formatModels(stats.Models)
		tokensStr := formatNumber(stats.TotalTokens)
		costStr := fmt.Sprintf("$%.2f", stats.TotalCost)
		messagesStr := fmt.Sprintf("%d", stats.MessageCount)

		row := fmt.Sprintf("%-12s  %-15s  %12s  %10s  %10s",
			dateStr, modelsStr, tokensStr, costStr, messagesStr)
		lines = append(lines, row)

		totalTokens += stats.TotalTokens
		totalCost += stats.TotalCost
		totalMessages += stats.MessageCount
	}

	// Total row
	lines = append(lines, LabelStyle.Render("─────────────────────────────────────────────────────────────────────"))
	totalRow := fmt.Sprintf("%-12s  %-15s  %12s  %10s  %10s",
		"Total", "", formatNumber(totalTokens), fmt.Sprintf("$%.2f", totalCost), fmt.Sprintf("%d", totalMessages))
	lines = append(lines, ValueStyle.Render(totalRow))

	lines = append(lines, "")
	lines = append(lines, HelpStyle.Render("Press q to exit"))

	return joinLines(lines)
}

// RenderMonthlyView renders the monthly aggregation view
func RenderMonthlyView(entries []models.UsageEntry, width int) string {
	if len(entries) == 0 {
		return WarningStyle.Render("No data available for monthly view\n\nPress q to exit")
	}

	// Aggregate by month
	monthlyStats := aggregateByMonth(entries)

	var lines []string
	lines = append(lines, TitleStyle.Render("📊 Monthly Usage Report"))
	lines = append(lines, "")

	// Header
	header := fmt.Sprintf("%-10s  %-15s  %-12s  %-10s  %-10s",
		"Month", "Models", "Tokens", "Cost", "Messages")
	lines = append(lines, HeaderStyle.Render(header))
	lines = append(lines, LabelStyle.Render("───────────────────────────────────────────────────────────────────"))

	// Rows
	totalTokens := 0
	totalCost := 0.0
	totalMessages := 0

	for _, stats := range monthlyStats {
		monthStr := stats.Month.Format("2006-01")
		modelsStr := formatModels(stats.Models)
		tokensStr := formatNumber(stats.TotalTokens)
		costStr := fmt.Sprintf("$%.2f", stats.TotalCost)
		messagesStr := fmt.Sprintf("%d", stats.MessageCount)

		row := fmt.Sprintf("%-10s  %-15s  %12s  %10s  %10s",
			monthStr, modelsStr, tokensStr, costStr, messagesStr)
		lines = append(lines, row)

		totalTokens += stats.TotalTokens
		totalCost += stats.TotalCost
		totalMessages += stats.MessageCount
	}

	// Total row
	lines = append(lines, LabelStyle.Render("───────────────────────────────────────────────────────────────────"))
	totalRow := fmt.Sprintf("%-10s  %-15s  %12s  %10s  %10s",
		"Total", "", formatNumber(totalTokens), fmt.Sprintf("$%.2f", totalCost), fmt.Sprintf("%d", totalMessages))
	lines = append(lines, ValueStyle.Render(totalRow))

	lines = append(lines, "")
	lines = append(lines, HelpStyle.Render("Press q to exit"))

	return joinLines(lines)
}

// aggregateByDay aggregates entries by day
func aggregateByDay(entries []models.UsageEntry) []DailyStats {
	dailyMap := make(map[string]*DailyStats)

	for _, entry := range entries {
		dateKey := entry.Timestamp.Format("2006-01-02")

		if dailyMap[dateKey] == nil {
			dailyMap[dateKey] = &DailyStats{
				Date:   entry.Timestamp.Truncate(24 * time.Hour),
				Models: make(map[string]bool),
			}
		}

		stats := dailyMap[dateKey]
		stats.TotalTokens += entry.DisplayTokens() // Match Python UI - only input + output
		stats.TotalCost += entry.CostUSD
		stats.MessageCount++
		stats.Models[models.NormaliseModelName(entry.Model)] = true
	}

	// Convert to slice and sort
	var result []DailyStats
	for _, stats := range dailyMap {
		result = append(result, *stats)
	}

	// Simple sort by date
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[i].Date.After(result[j].Date) {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	return result
}

// aggregateByMonth aggregates entries by month
func aggregateByMonth(entries []models.UsageEntry) []MonthlyStats {
	monthlyMap := make(map[string]*MonthlyStats)

	for _, entry := range entries {
		monthKey := entry.Timestamp.Format("2006-01")

		if monthlyMap[monthKey] == nil {
			year, month, _ := entry.Timestamp.Date()
			monthlyMap[monthKey] = &MonthlyStats{
				Month:  time.Date(year, month, 1, 0, 0, 0, 0, time.UTC),
				Models: make(map[string]bool),
			}
		}

		stats := monthlyMap[monthKey]
		stats.TotalTokens += entry.DisplayTokens() // Match Python UI - only input + output
		stats.TotalCost += entry.CostUSD
		stats.MessageCount++
		stats.Models[models.NormaliseModelName(entry.Model)] = true
	}

	// Convert to slice and sort
	var result []MonthlyStats
	for _, stats := range monthlyMap {
		result = append(result, *stats)
	}

	// Simple sort by month
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[i].Month.After(result[j].Month) {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	return result
}

// formatModels formats model names for table display
func formatModels(models map[string]bool) string {
	if len(models) == 0 {
		return "-"
	}

	var modelNames []string
	for model := range models {
		switch model {
		case "claude-sonnet-4", "claude-sonnet-4-5":
			modelNames = append(modelNames, "Sonnet")
		case "claude-opus-4", "claude-3-opus":
			modelNames = append(modelNames, "Opus")
		case "claude-3-haiku", "claude-3-5-haiku":
			modelNames = append(modelNames, "Haiku")
		}
	}

	if len(modelNames) == 0 {
		return "Mixed"
	}

	// Simple join
	var result strings.Builder
	result.WriteString(modelNames[0])
	for i := 1; i < len(modelNames); i++ {
		result.WriteString("/" + modelNames[i])
	}

	return result.String()
}

// joinLines joins lines with newlines
func joinLines(lines []string) string {
	var result strings.Builder
	for i, line := range lines {
		result.WriteString(line)
		if i < len(lines)-1 {
			result.WriteString("\n")
		}
	}
	return result.String()
}
