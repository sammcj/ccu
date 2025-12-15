package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sammcj/ccu/internal/models"
	"github.com/sammcj/ccu/internal/pricing"
)

// ReportStats holds aggregated statistics for a time period
type ReportStats struct {
	Period              time.Time
	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int
	TotalTokens         int
	TotalCost           float64
	Models              map[string]bool // Full model names
}

// GenerateDailyReport generates a static daily usage report
func GenerateDailyReport(entries []models.UsageEntry, timezone *time.Location) string {
	if len(entries) == 0 {
		return "No usage data found.\n"
	}

	stats := aggregateForReport(entries, "daily", timezone)
	return renderReport(stats, "Daily", timezone)
}

// GenerateMonthlyReport generates a static monthly usage report
func GenerateMonthlyReport(entries []models.UsageEntry, timezone *time.Location) string {
	if len(entries) == 0 {
		return "No usage data found.\n"
	}

	stats := aggregateForReport(entries, "monthly", timezone)
	return renderReport(stats, "Monthly", timezone)
}

// aggregateForReport aggregates entries by period (daily or monthly)
func aggregateForReport(entries []models.UsageEntry, period string, timezone *time.Location) []ReportStats {
	statsMap := make(map[string]*ReportStats)

	for _, entry := range entries {
		// Convert to local timezone for grouping
		localTime := entry.Timestamp.In(timezone)

		var key string
		var periodTime time.Time

		if period == "daily" {
			key = localTime.Format("2006-01-02")
			year, month, day := localTime.Date()
			periodTime = time.Date(year, month, day, 0, 0, 0, 0, timezone)
		} else {
			key = localTime.Format("2006-01")
			year, month, _ := localTime.Date()
			periodTime = time.Date(year, month, 1, 0, 0, 0, 0, timezone)
		}

		if statsMap[key] == nil {
			statsMap[key] = &ReportStats{
				Period: periodTime,
				Models: make(map[string]bool),
			}
		}

		s := statsMap[key]
		s.InputTokens += entry.InputTokens
		s.OutputTokens += entry.OutputTokens
		s.CacheCreationTokens += entry.CacheCreationTokens
		s.CacheReadTokens += entry.CacheReadTokens
		s.TotalTokens += entry.TotalTokens()
		s.TotalCost += entry.CostUSD
		s.Models[entry.Model] = true
	}

	// Convert to slice and sort by period
	result := make([]ReportStats, 0, len(statsMap))
	for _, s := range statsMap {
		result = append(result, *s)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Period.Before(result[j].Period)
	})

	return result
}

// renderReport renders the report as a formatted table
func renderReport(stats []ReportStats, periodType string, timezone *time.Location) string {
	var sb strings.Builder

	// Get timezone name
	tzName := timezone.String()

	// Header
	sb.WriteString(fmt.Sprintf("Claude Code Token Usage Report - %s (%s)\n", periodType, tzName))
	sb.WriteString(strings.Repeat("─", 120) + "\n")

	// Column headers
	if periodType == "Monthly" {
		sb.WriteString(fmt.Sprintf("%-10s  %-32s  %12s  %12s  %14s  %16s  %16s  %12s\n",
			"Month", "Models", "Input", "Output", "Cache Create", "Cache Read", "Total Tokens", "Est. Cost"))
	} else {
		sb.WriteString(fmt.Sprintf("%-12s  %-32s  %12s  %12s  %14s  %16s  %16s  %12s\n",
			"Date", "Models", "Input", "Output", "Cache Create", "Cache Read", "Total Tokens", "Est. Cost"))
	}
	sb.WriteString(strings.Repeat("─", 120) + "\n")

	// Totals
	var totalInput, totalOutput, totalCacheCreate, totalCacheRead, totalTokens int
	var totalCost float64
	allModels := make(map[string]bool)

	// Rows
	for _, s := range stats {
		var periodStr string
		if periodType == "Monthly" {
			periodStr = s.Period.Format("2006-01")
			// Mark partial months
			now := time.Now().In(timezone)
			if s.Period.Year() == now.Year() && s.Period.Month() == now.Month() {
				periodStr += " *"
			}
		} else {
			periodStr = s.Period.Format("2006-01-02")
		}

		modelsStr := formatModelList(s.Models)
		for m := range s.Models {
			allModels[m] = true
		}

		if periodType == "Monthly" {
			sb.WriteString(fmt.Sprintf("%-10s  %-32s  %12s  %12s  %14s  %16s  %16s  %12s\n",
				periodStr,
				truncate(modelsStr, 32),
				formatNumber(s.InputTokens),
				formatNumber(s.OutputTokens),
				formatNumber(s.CacheCreationTokens),
				formatNumber(s.CacheReadTokens),
				formatNumber(s.TotalTokens),
				fmt.Sprintf("$%.2f", s.TotalCost)))
		} else {
			sb.WriteString(fmt.Sprintf("%-12s  %-32s  %12s  %12s  %14s  %16s  %16s  %12s\n",
				periodStr,
				truncate(modelsStr, 32),
				formatNumber(s.InputTokens),
				formatNumber(s.OutputTokens),
				formatNumber(s.CacheCreationTokens),
				formatNumber(s.CacheReadTokens),
				formatNumber(s.TotalTokens),
				fmt.Sprintf("$%.2f", s.TotalCost)))
		}

		totalInput += s.InputTokens
		totalOutput += s.OutputTokens
		totalCacheCreate += s.CacheCreationTokens
		totalCacheRead += s.CacheReadTokens
		totalTokens += s.TotalTokens
		totalCost += s.TotalCost
	}

	// Total row
	sb.WriteString(strings.Repeat("─", 120) + "\n")
	if periodType == "Monthly" {
		sb.WriteString(fmt.Sprintf("%-10s  %-32s  %12s  %12s  %14s  %16s  %16s  %12s\n",
			"TOTAL", "",
			formatNumber(totalInput),
			formatNumber(totalOutput),
			formatNumber(totalCacheCreate),
			formatNumber(totalCacheRead),
			formatNumber(totalTokens),
			fmt.Sprintf("$%.2f", totalCost)))
	} else {
		sb.WriteString(fmt.Sprintf("%-12s  %-32s  %12s  %12s  %14s  %16s  %16s  %12s\n",
			"TOTAL", "",
			formatNumber(totalInput),
			formatNumber(totalOutput),
			formatNumber(totalCacheCreate),
			formatNumber(totalCacheRead),
			formatNumber(totalTokens),
			fmt.Sprintf("$%.2f", totalCost)))
	}

	// Footer with model list and pricing note
	sb.WriteString("\n")
	sb.WriteString("Models used:\n")
	for model := range allModels {
		sb.WriteString(fmt.Sprintf("  • %s\n", model))
	}

	sb.WriteString("\n")
	sb.WriteString("* Partial period (current month/day)\n")
	sb.WriteString(fmt.Sprintf("Pricing: %s\n", pricing.GetPricingSource()))

	return sb.String()
}

// formatModelList formats model names for display
func formatModelList(models map[string]bool) string {
	if len(models) == 0 {
		return "-"
	}

	// Extract unique short names
	shortNames := make(map[string]bool)
	for model := range models {
		short := getShortModelName(model)
		shortNames[short] = true
	}

	// Convert to sorted slice
	names := make([]string, 0, len(shortNames))
	for name := range shortNames {
		names = append(names, name)
	}
	sort.Strings(names)

	return strings.Join(names, ", ")
}

// getShortModelName returns a short display name for a model
func getShortModelName(model string) string {
	// Map full model IDs to short display names
	switch {
	case strings.Contains(model, "opus"):
		return "opus"
	case strings.Contains(model, "sonnet"):
		return "sonnet"
	case strings.Contains(model, "haiku"):
		return "haiku"
	default:
		return model
	}
}

// truncate truncates a string to maxLen with ellipsis
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
