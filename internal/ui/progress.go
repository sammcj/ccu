package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// RenderProgressBar renders a text-based progress bar
func RenderProgressBar(current, total int, width int, label string) string {
	if width < 10 {
		width = 10
	}

	percent := 0.0
	if total > 0 {
		percent = (float64(current) / float64(total)) * 100
		if percent > 100 {
			percent = 100
		}
	}

	// Calculate filled portion
	barWidth := width - 2 // Account for brackets
	filled := int((percent / 100) * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}

	// Build bar
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	// Apply colour based on percentage
	style := GetProgressStyle(percent)

	// Format the progress bar with label and percentage
	return fmt.Sprintf("%s [%s] %.1f%%",
		label,
		style.Render(bar),
		percent,
	)
}

// RenderProgressBarNeutral renders a text-based progress bar with neutral white colour
func RenderProgressBarNeutral(current, total int, width int, label string) string {
	if width < 10 {
		width = 10
	}

	percent := 0.0
	if total > 0 {
		percent = (float64(current) / float64(total)) * 100
		if percent > 100 {
			percent = 100
		}
	}

	// Calculate filled portion
	barWidth := width - 2 // Account for brackets
	filled := int((percent / 100) * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}

	// Build bar
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	// Use neutral white colour
	style := lipgloss.NewStyle().Foreground(ColorWhite)

	// Format the progress bar with label and percentage
	return fmt.Sprintf("%s [%s] %.1f%%",
		label,
		style.Render(bar),
		percent,
	)
}

// RenderProgressBarWithValues renders a progress bar with current/total values
func RenderProgressBarWithValues(current, total int, width int, label, unit string) string {
	if width < 10 {
		width = 10
	}

	percent := 0.0
	if total > 0 {
		percent = (float64(current) / float64(total)) * 100
		if percent > 100 {
			percent = 100
		}
	}

	// Calculate filled portion
	barWidth := width - 2
	filled := int((percent / 100) * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}

	// Build bar
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	// Apply colour
	style := GetProgressStyle(percent)

	// Format with values
	valueStr := fmt.Sprintf("%s / %s %s", formatNumber(current), formatNumber(total), unit)

	return fmt.Sprintf("%s\n[%s] %.1f%%\n%s",
		label,
		style.Render(bar),
		percent,
		ValueStyle.Render(valueStr),
	)
}

// RenderFloatProgressBar renders a progress bar for float values
func RenderFloatProgressBar(current, total float64, width int, label, unit string) string {
	if width < 10 {
		width = 10
	}

	percent := 0.0
	if total > 0 {
		percent = (current / total) * 100
		if percent > 100 {
			percent = 100
		}
	}

	// Calculate filled portion
	barWidth := width - 2
	filled := int((percent / 100) * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}

	// Build bar
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	// Apply colour
	style := GetProgressStyle(percent)

	// Format with values
	valueStr := fmt.Sprintf("%.1f / %.1f %s", current, total, unit)

	return fmt.Sprintf("%s\n[%s] %.1f%%\n%s",
		label,
		style.Render(bar),
		percent,
		ValueStyle.Render(valueStr),
	)
}

// formatNumber formats large numbers with commas
func formatNumber(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}

	// Add commas
	var result strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result.WriteRune(',')
		}
		result.WriteRune(c)
	}

	return result.String()
}

// RenderMetric renders a simple metric with label and value
func RenderMetric(label, value string, style lipgloss.Style) string {
	return fmt.Sprintf("%s %s",
		LabelStyle.Render(label+":"),
		style.Render(value),
	)
}

// RenderMetricWithUnit renders a metric with a unit
func RenderMetricWithUnit(label string, value float64, unit string, style lipgloss.Style) string {
	return fmt.Sprintf("%s %s",
		LabelStyle.Render(label+":"),
		style.Render(fmt.Sprintf("%.2f %s", value, unit)),
	)
}
