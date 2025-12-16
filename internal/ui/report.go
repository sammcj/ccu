package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sammcj/ccu/internal/models"
	"github.com/sammcj/ccu/internal/pricing"
)

// ModelStats holds token statistics for a single model
type ModelStats struct {
	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int
	TotalTokens         int
	TotalCost           float64
}

// ReportStats holds aggregated statistics for a time period
type ReportStats struct {
	Period      time.Time
	ModelStats  map[string]*ModelStats // model name -> stats
	TotalTokens int                    // period total for sorting
}

// GenerateDailyReport generates a static daily usage report
func GenerateDailyReport(entries []models.UsageEntry, timezone *time.Location) string {
	if len(entries) == 0 {
		return "No usage data found.\n"
	}

	stats := aggregateForReport(entries, "daily", timezone)
	return renderReport(stats, "Daily", timezone)
}

// GenerateWeeklyReport generates a static weekly usage report
func GenerateWeeklyReport(entries []models.UsageEntry, timezone *time.Location) string {
	if len(entries) == 0 {
		return "No usage data found.\n"
	}

	stats := aggregateForReport(entries, "weekly", timezone)
	return renderReport(stats, "Weekly", timezone)
}

// GenerateMonthlyReport generates a static monthly usage report
func GenerateMonthlyReport(entries []models.UsageEntry, timezone *time.Location) string {
	if len(entries) == 0 {
		return "No usage data found.\n"
	}

	stats := aggregateForReport(entries, "monthly", timezone)
	return renderReport(stats, "Monthly", timezone)
}

// aggregateForReport aggregates entries by period (daily or monthly) and by model
func aggregateForReport(entries []models.UsageEntry, period string, timezone *time.Location) []ReportStats {
	statsMap := make(map[string]*ReportStats)

	for _, entry := range entries {
		// Convert to local timezone for grouping
		localTime := entry.Timestamp.In(timezone)

		var key string
		var periodTime time.Time

		switch period {
		case "daily":
			key = localTime.Format("2006-01-02")
			year, month, day := localTime.Date()
			periodTime = time.Date(year, month, day, 0, 0, 0, 0, timezone)
		case "weekly":
			// Use ISO week number (weeks start on Monday)
			year, week := localTime.ISOWeek()
			key = fmt.Sprintf("%d-W%02d", year, week)
			// Calculate the Monday of this ISO week
			periodTime = getWeekStart(localTime, timezone)
		default: // monthly
			key = localTime.Format("2006-01")
			year, month, _ := localTime.Date()
			periodTime = time.Date(year, month, 1, 0, 0, 0, 0, timezone)
		}

		if statsMap[key] == nil {
			statsMap[key] = &ReportStats{
				Period:     periodTime,
				ModelStats: make(map[string]*ModelStats),
			}
		}

		s := statsMap[key]

		// Get or create model stats
		modelName := entry.Model
		if s.ModelStats[modelName] == nil {
			s.ModelStats[modelName] = &ModelStats{}
		}
		ms := s.ModelStats[modelName]

		ms.InputTokens += entry.InputTokens
		ms.OutputTokens += entry.OutputTokens
		ms.CacheCreationTokens += entry.CacheCreationTokens
		ms.CacheReadTokens += entry.CacheReadTokens
		ms.TotalTokens += entry.TotalTokens()
		ms.TotalCost += entry.CostUSD

		s.TotalTokens += entry.TotalTokens()
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
	sb.WriteString(strings.Repeat("─", 140) + "\n")

	// Column headers - use wider model column for full names
	var periodLabel string
	var periodWidth int
	switch periodType {
	case "Monthly":
		periodLabel = "Month"
		periodWidth = 10
	case "Weekly":
		periodLabel = "Week"
		periodWidth = 12
	default: // Daily
		periodLabel = "Date"
		periodWidth = 12
	}
	sb.WriteString(fmt.Sprintf("%-*s  %-30s  %12s  %12s  %16s  %18s  %16s  %12s\n",
		periodWidth, periodLabel, "Model", "Input", "Output", "Cache Create", "Cache Read", "Total Tokens", "Est. Cost"))
	sb.WriteString(strings.Repeat("─", 140) + "\n")

	// Grand totals
	var totalInput, totalOutput, totalCacheCreate, totalCacheRead, totalTokens int
	var totalCost float64

	// Track partial periods
	hasPartialPeriod := false

	// Rows - one per model per period
	for _, s := range stats {
		var periodStr string
		isPartial := false
		now := time.Now().In(timezone)

		switch periodType {
		case "Monthly":
			periodStr = s.Period.Format("2006-01")
			if s.Period.Year() == now.Year() && s.Period.Month() == now.Month() {
				periodStr += " *"
				isPartial = true
				hasPartialPeriod = true
			}
		case "Weekly":
			// Format as YYYY-Www
			year, week := s.Period.ISOWeek()
			periodStr = fmt.Sprintf("%d-W%02d", year, week)
			// Check if this is the current week
			nowYear, nowWeek := now.ISOWeek()
			if year == nowYear && week == nowWeek {
				periodStr += " *"
				isPartial = true
				hasPartialPeriod = true
			}
		default: // Daily
			periodStr = s.Period.Format("2006-01-02")
			y1, m1, d1 := s.Period.Date()
			y2, m2, d2 := now.Date()
			if y1 == y2 && m1 == m2 && d1 == d2 {
				periodStr += " *"
				isPartial = true
				hasPartialPeriod = true
			}
		}

		// Get sorted model names for consistent ordering
		modelNames := getSortedModelNames(s.ModelStats)

		// Print each model on its own row
		for i, modelName := range modelNames {
			ms := s.ModelStats[modelName]

			// Only show period on first row for this period
			displayPeriod := ""
			if i == 0 {
				displayPeriod = periodStr
			}

			sb.WriteString(fmt.Sprintf("%-*s  %-30s  %12s  %12s  %16s  %18s  %16s  %12s\n",
				periodWidth,
				displayPeriod,
				truncate(modelName, 30),
				formatNumber(ms.InputTokens),
				formatNumber(ms.OutputTokens),
				formatNumber(ms.CacheCreationTokens),
				formatNumber(ms.CacheReadTokens),
				formatNumber(ms.TotalTokens),
				fmt.Sprintf("$%.2f", ms.TotalCost)))

			// Accumulate grand totals
			totalInput += ms.InputTokens
			totalOutput += ms.OutputTokens
			totalCacheCreate += ms.CacheCreationTokens
			totalCacheRead += ms.CacheReadTokens
			totalTokens += ms.TotalTokens
			totalCost += ms.TotalCost
		}

		// Add period subtotal if multiple models
		if len(modelNames) > 1 {
			var periodInput, periodOutput, periodCacheCreate, periodCacheRead, periodTokens int
			var periodCost float64
			for _, ms := range s.ModelStats {
				periodInput += ms.InputTokens
				periodOutput += ms.OutputTokens
				periodCacheCreate += ms.CacheCreationTokens
				periodCacheRead += ms.CacheReadTokens
				periodTokens += ms.TotalTokens
				periodCost += ms.TotalCost
			}

			subtotalLabel := "Subtotal"
			if isPartial {
				subtotalLabel = "Subtotal *"
			}

			sb.WriteString(fmt.Sprintf("%-*s  %-30s  %12s  %12s  %16s  %18s  %16s  %12s\n",
				periodWidth,
				"",
				subtotalLabel,
				formatNumber(periodInput),
				formatNumber(periodOutput),
				formatNumber(periodCacheCreate),
				formatNumber(periodCacheRead),
				formatNumber(periodTokens),
				fmt.Sprintf("$%.2f", periodCost)))
		}

		// Add blank line between periods for readability
		sb.WriteString("\n")
	}

	// Grand total row
	sb.WriteString(strings.Repeat("─", 140) + "\n")
	sb.WriteString(fmt.Sprintf("%-*s  %-30s  %12s  %12s  %16s  %18s  %16s  %12s\n",
		periodWidth,
		"TOTAL", "",
		formatNumber(totalInput),
		formatNumber(totalOutput),
		formatNumber(totalCacheCreate),
		formatNumber(totalCacheRead),
		formatNumber(totalTokens),
		fmt.Sprintf("$%.2f", totalCost)))

	// Footer
	sb.WriteString("\n")
	if hasPartialPeriod {
		sb.WriteString("* Partial period (current month/week/day)\n")
	}
	sb.WriteString(fmt.Sprintf("Pricing: %s\n", pricing.GetPricingSource()))

	return sb.String()
}

// getSortedModelNames returns model names sorted alphabetically
func getSortedModelNames(modelStats map[string]*ModelStats) []string {
	names := make([]string, 0, len(modelStats))
	for name := range modelStats {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// getWeekStart returns the Monday of the ISO week containing the given time
func getWeekStart(t time.Time, timezone *time.Location) time.Time {
	// Get the weekday (Sunday=0, Monday=1, ..., Saturday=6)
	weekday := int(t.Weekday())
	// Convert to ISO weekday (Monday=0, ..., Sunday=6)
	if weekday == 0 {
		weekday = 6 // Sunday becomes 6
	} else {
		weekday-- // Monday becomes 0, etc.
	}
	// Subtract days to get to Monday
	monday := t.AddDate(0, 0, -weekday)
	year, month, day := monday.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, timezone)
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
