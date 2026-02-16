package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/sammcj/ccu/internal/analysis"
	"github.com/sammcj/ccu/internal/models"
	"github.com/sammcj/ccu/internal/oauth"
)

// Column positions (1-based) for ANSI cursor positioning
// Using escape sequence \x1b[<n>G to move cursor to absolute column n
const (
	colPosLabel  = 5  // Column where label starts (after emoji)
	colPosBar    = 27 // Column where progress bar starts
	colPosValue  = 74 // Column where value starts (after 47-char bar)
	colPosSuffix = 86 // Column where suffix starts
)

// formatRow creates a consistently formatted progress bar row.
// Uses ANSI escape sequences to position cursor at fixed columns,
// ensuring alignment regardless of how the terminal renders emoji widths.
func formatRow(emoji, label, bar, value, suffix string) string {
	// \x1b[2K clears the entire line first (prevents ghost characters from previous frames)
	// \x1b[nG moves cursor to column n (1-based)
	var b strings.Builder
	b.WriteString("\x1b[2K") // Clear entire line first
	b.WriteString(emoji)
	b.WriteString(fmt.Sprintf("\x1b[%dG%s", colPosLabel, label))
	b.WriteString(fmt.Sprintf("\x1b[%dG%s", colPosBar, bar))
	b.WriteString(fmt.Sprintf("\x1b[%dG%s", colPosValue, value))
	if suffix != "" {
		b.WriteString(fmt.Sprintf("\x1b[%dG%s", colPosSuffix, suffix))
	}
	return b.String()
}

// DashboardData contains all data needed to render the dashboard
type DashboardData struct {
	Config                 *models.Config
	Limits                 models.Limits
	CurrentSession         *models.SessionBlock
	AllSessions            []models.SessionBlock
	OAuthData              *oauth.UsageData // Optional OAuth-fetched data
	OAuthUnavailableReason string           // Reason OAuth is unavailable (for fallback display)
}

// RenderDashboard renders the realtime dashboard in a single-column layout
func RenderDashboard(data DashboardData) string {
	if data.CurrentSession == nil {
		return WarningStyle.Render("No active session found\n\nPress q or Ctrl-C to exit")
	}

	now := time.Now()
	var output []string

	const barWidth = 45

	// Weekly usage (if enabled) -- OAuth-only, no JSONL fallback
	if data.Config.ShowWeekly && data.OAuthData != nil && (data.OAuthData.SevenDaySonnet != nil || data.OAuthData.SevenDayOpus != nil) {
		output = append(output, renderWeeklyUsageFromOAuth(data.OAuthData, data.Limits, barWidth)...)
	}

	// Calculate burn rates (tokens and cost)
	burnRate := analysis.CalculateBurnRate(data.AllSessions, now)
	costBurnRate := calculateCostBurnRateFromSessions(data.AllSessions, now)

	// Get session reset time for burn rate display
	var sessionResetTime time.Time
	if data.OAuthData != nil {
		sessionResetTime, _ = oauth.ParseResetTime(data.OAuthData.FiveHour.ResetsAt)
		// Adjust if session rolled over
		if !sessionResetTime.After(now) {
			sessionResetTime = sessionResetTime.Add(5 * time.Hour)
		}
	} else if data.CurrentSession != nil {
		sessionResetTime = data.CurrentSession.EndTime
	}

	// Show burn rates on one line
	output = append(output, renderBurnRates(burnRate, costBurnRate, data.Limits, barWidth, sessionResetTime))

	// Get session distribution for appending to session usage line
	sessionDistribution := getSessionDistributionString(data.CurrentSession)

	// If OAuth available, show OAuth-based session metrics; otherwise degraded fallback
	if data.OAuthData != nil {
		output = append(output, renderSessionMetricsFromOAuth(data.OAuthData, sessionDistribution, barWidth, now)...)
	} else {
		output = append(output, renderSessionFallback(data.CurrentSession, sessionDistribution, now, data.OAuthUnavailableReason)...)
	}

	output = append(output, "") // Blank line before prediction

	// Prediction -- OAuth-only
	if data.OAuthData != nil {
		output = append(output, renderPredictionWithOAuth(data.OAuthData, data.CurrentSession, costBurnRate, data.Limits, now, data.Config.ShowWeekly))
	}

	// Limit warnings -- OAuth-only
	if data.OAuthData != nil {
		warning := renderOAuthLimitWarning(data.OAuthData, now)
		if warning != "" {
			output = append(output, warning)
		}
	}

	return strings.Join(output, "\n")
}

// getSessionDistributionString returns just the distribution part without the label
// Uses cost-based distribution as this is more meaningful than token counts
// (e.g., Haiku is cheap so high token counts don't reflect actual usage impact)
func getSessionDistributionString(session *models.SessionBlock) string {
	if session == nil {
		return ""
	}

	// Calculate cost distribution by model
	modelCosts := make(map[string]float64)
	totalCost := 0.0

	for _, entry := range session.Entries {
		modelCosts[entry.Model] += entry.CostUSD
		totalCost += entry.CostUSD
	}

	if totalCost == 0 {
		return ""
	}

	// Create sorted list of models
	type modelData struct {
		name    string
		percent float64
	}

	var sortedModels []modelData
	for model, cost := range modelCosts {
		if cost > 0 {
			percent := (cost / totalCost) * 100
			sortedModels = append(sortedModels, modelData{
				name:    model,
				percent: percent,
			})
		}
	}

	// Sort by percentage descending (highest usage first)
	slices.SortFunc(sortedModels, func(a, b modelData) int {
		if a.percent > b.percent {
			return -1
		} else if a.percent < b.percent {
			return 1
		}
		return 0
	})

	// Build distribution string (without label) with colour-coded model names
	var parts []string
	for _, m := range sortedModels {
		modelName := formatModelNameSimple(m.name)
		modelStyle := GetModelStyle(m.name)
		colouredModel := modelStyle.Render(modelName)
		parts = append(parts, fmt.Sprintf("%s: %.1f%%", colouredModel, m.percent))
	}

	return "[" + strings.Join(parts, ", ") + "]"
}

// renderBurnRates renders token and cost burn rates on one line
func renderBurnRates(tokenBurnRate, costBurnRate float64, limits models.Limits, barWidth int, sessionResetTime time.Time) string {
	// Calculate what percentage of limit the burn rate represents
	// For a reasonable scale, let's assume max comfortable burn rate is hitting limit in 2 hours
	// So: maxBurnRate = limit / 120 minutes

	// Token burn rate percentage (green at low, red at high)
	tokenMaxRate := 0.0
	if limits.CostLimitUSD > 0 {
		// Use cost as primary metric; estimate ~3000 tokens per dollar for rough scaling
		tokenMaxRate = (limits.CostLimitUSD * 3000) / 120.0 // tokens per minute to hit limit in 2 hours
	}

	tokenPercent := 0.0
	if tokenMaxRate > 0 && tokenBurnRate > 0 {
		tokenPercent = (tokenBurnRate / tokenMaxRate) * 100
		if tokenPercent > 100 {
			tokenPercent = 100
		}
	}

	// Cost burn rate percentage
	costMaxRate := 0.0
	if limits.CostLimitUSD > 0 {
		costMaxRate = limits.CostLimitUSD / 120.0 // dollars per minute to hit limit in 2 hours
	}

	costPercent := 0.0
	if costMaxRate > 0 && costBurnRate > 0 {
		costPercent = (costBurnRate / costMaxRate) * 100
		if costPercent > 100 {
			costPercent = 100
		}
	}

	// Create normal sized bars for each metric
	// First bar aligns with other bars, second starts where second column info starts
	normalBarWidth := barWidth

	tokenFilled := min(int((tokenPercent/100)*float64(normalBarWidth-2)), normalBarWidth-2)
	if tokenFilled < 0 {
		tokenFilled = 0
	}
	tokenBar := "[" + strings.Repeat("█", tokenFilled) + strings.Repeat("░", normalBarWidth-2-tokenFilled) + "]"
	tokenStyle := GetPercentageStyle(tokenPercent)

	costFilled := min(int((costPercent/100)*float64(normalBarWidth-2)), normalBarWidth-2)
	if costFilled < 0 {
		costFilled = 0
	}
	costBar := "[" + strings.Repeat("█", costFilled) + strings.Repeat("░", normalBarWidth-2-costFilled) + "]"
	costStyle := GetPercentageStyle(costPercent)

	// Cost burn rate in dollars per hour for readability
	costPerHour := costBurnRate * 60.0

	// Colourise the burn rate values to match bar colours
	tokenValueStyle := GetPercentageStyle(tokenPercent)
	costValueStyle := GetPercentageStyle(costPercent)

	// Session reset time
	resetStr := ""
	if !sessionResetTime.IsZero() {
		whiteStyle := lipgloss.NewStyle().Foreground(ColorWhite)
		resetStr = whiteStyle.Render(fmt.Sprintf("[Resets: %s]", sessionResetTime.Local().Format("3:04 PM")))
	}

	// Format using formatRow for consistent alignment
	tokenLine := formatRow(
		"🔥",
		"Burn Rate - Tokens:",
		tokenStyle.Render(tokenBar),
		tokenValueStyle.Render(fmt.Sprintf("%.0f/min", tokenBurnRate)),
		"",
	)
	costLine := formatRow(
		"💸",
		"Burn Rate - Cost:",
		costStyle.Render(costBar),
		costValueStyle.Render(fmt.Sprintf("$%.2f/hr", costPerHour)),
		resetStr,
	)
	return tokenLine + "\n" + costLine
}

// Helper functions

// formatModelNameSimple returns simplified model names for display.
// Converts full API names like "claude-opus-4-5-20251101" to "Opus 4.5"
func formatModelNameSimple(model string) string {
	name := strings.TrimPrefix(model, "claude-")

	// Remove date suffix if present (8-digit date like -20251101)
	parts := strings.Split(name, "-")
	if len(parts) > 0 {
		last := parts[len(parts)-1]
		if len(last) == 8 && isNumeric(last) {
			parts = parts[:len(parts)-1]
			name = strings.Join(parts, "-")
		}
	}

	// Find model family and extract version
	families := []string{"opus", "sonnet", "haiku"}
	for _, family := range families {
		if strings.Contains(name, family) {
			_, after, _ := strings.Cut(name, family)
			afterFamily := strings.TrimPrefix(after, "-")

			// Convert version dashes to dots (e.g., "4-5" -> "4.5")
			version := strings.ReplaceAll(afterFamily, "-", ".")

			familyName := strings.ToUpper(family[:1]) + family[1:]
			if version != "" {
				return familyName + " " + version
			}
			return familyName
		}
	}

	return model
}

// isNumeric checks if a string contains only digits
func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

// renderSessionFallback renders degraded session info when OAuth is unavailable.
// Shows raw cost, message count, time before reset, and session distribution
// but no progress bars or percentages (those require OAuth for accurate limits).
func renderSessionFallback(session *models.SessionBlock, sessionDistribution string, now time.Time, oauthReason string) []string {
	var lines []string
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))
	whiteStyle := lipgloss.NewStyle().Foreground(ColorWhite)

	// Raw cost and message count
	costStr := fmt.Sprintf("$%.2f", session.CostUSD)
	msgStr := fmt.Sprintf("%d messages", session.MessageCount)
	distStr := ""
	if sessionDistribution != "" {
		distStr = " " + sessionDistribution
	}
	lines = append(lines, fmt.Sprintf("  💸 Session: %s | %s%s",
		whiteStyle.Render(costStr),
		whiteStyle.Render(msgStr),
		distStr,
	))

	// Time before reset (still computable from JSONL session blocks)
	remaining := session.RemainingDuration(now)
	if remaining > 0 {
		lines = append(lines, fmt.Sprintf("  ⏱️  Time before reset: %s",
			whiteStyle.Render(fmt.Sprintf("%.1f hours remaining", remaining.Hours())),
		))
	} else if session.EndTime.Before(now) {
		lines = append(lines, dimStyle.Render("  (session data is from a previous session - run Claude Code to refresh)"))
	}

	// OAuth unavailable notice
	notice := "OAuth unavailable - usage percentages unavailable"
	if oauthReason != "" {
		notice = fmt.Sprintf("OAuth unavailable: %s", oauthReason)
	}
	lines = append(lines, dimStyle.Render(fmt.Sprintf("  (%s)", notice)))

	return lines
}

// renderWeeklyUsageFromOAuth renders weekly usage from OAuth data (matching JSONL style)
func renderWeeklyUsageFromOAuth(oauthData *oauth.UsageData, limits models.Limits, barWidth int) []string {
	var lines []string

	// Get weekly limits based on plan
	weeklyLimits := models.GetWeeklyLimits(strings.ToLower(limits.PlanName))

	// Combined "All models" weekly limit (always present in API response)
	{
		allModelsPercent := oauthData.SevenDay.Utilisation
		filled := min(int((allModelsPercent/100)*float64(barWidth-2)), barWidth-2)
		bar := "[" + strings.Repeat("█", filled) + strings.Repeat("░", barWidth-2-filled) + "]"

		// Parse reset time
		resetTime, err := oauth.ParseResetTime(oauthData.SevenDay.ResetsAt)
		resetStr := ""
		whiteStyle := lipgloss.NewStyle().Foreground(ColorWhite)
		if err == nil {
			resetStr = whiteStyle.Render(fmt.Sprintf("[Resets: %s %s]",
				resetTime.Local().Format("Mon"),
				resetTime.Local().Format("3:04 PM")))
		}

		// Use green-to-red gradient for both bar and percentage
		barStyle := GetPercentageStyle(allModelsPercent)
		percentStyle := GetPercentageStyle(allModelsPercent)

		line := formatRow(
			"🗓️",
			"Weekly - All Models:",
			barStyle.Render(bar),
			percentStyle.Render(fmt.Sprintf("%.1f%%", allModelsPercent)),
			resetStr,
		)
		lines = append(lines, line)
	}

	// Sonnet
	if oauthData.SevenDaySonnet != nil {
		sonnetPercent := oauthData.SevenDaySonnet.Utilisation
		// Convert to filled bar amount
		filled := min(int((sonnetPercent/100)*float64(barWidth-2)), barWidth-2)
		bar := "[" + strings.Repeat("█", filled) + strings.Repeat("░", barWidth-2-filled) + "]"

		limitHours := weeklyLimits.SonnetHours
		usedHours := (sonnetPercent / 100) * limitHours

		// Use green-to-red gradient for both bar and percentage
		barStyle := GetPercentageStyle(sonnetPercent)
		percentStyle := GetPercentageStyle(sonnetPercent)

		// Hours value in parentheses
		hoursValue := GetPercentageStyle(sonnetPercent).Render(fmt.Sprintf("(%.1f / %.1f hrs)", usedHours, limitHours))

		line := formatRow(
			"🗓️",
			"Weekly - Sonnet:",
			barStyle.Render(bar),
			percentStyle.Render(fmt.Sprintf("%.1f%%", sonnetPercent)),
			hoursValue,
		)
		lines = append(lines, line)
	}

	// Opus - only show if API returns SevenDayOpus with a reset time (indicates enforced limit)
	// This auto-detects when Anthropic enables Opus weekly limits without requiring code changes
	if oauthData.SevenDayOpus != nil && oauthData.SevenDayOpus.ResetsAt != nil {
		opusPercent := oauthData.SevenDayOpus.Utilisation
		filled := min(int((opusPercent/100)*float64(barWidth-2)), barWidth-2)
		bar := "[" + strings.Repeat("█", filled) + strings.Repeat("░", barWidth-2-filled) + "]"

		// Use green-to-red gradient for both bar and percentage
		barStyle := GetPercentageStyle(opusPercent)
		percentStyle := GetPercentageStyle(opusPercent)

		// Show reset time instead of fake hours (we don't know Anthropic's actual limit)
		resetTime, err := oauth.ParseResetTime(*oauthData.SevenDayOpus.ResetsAt)
		resetStr := ""
		if err == nil {
			whiteStyle := lipgloss.NewStyle().Foreground(ColorWhite)
			resetStr = whiteStyle.Render(fmt.Sprintf("[Resets: %s %s]",
				resetTime.Local().Format("Mon"),
				resetTime.Local().Format("3:04 PM")))
		}

		line := formatRow(
			"🗓️",
			"Weekly - Opus:",
			barStyle.Render(bar),
			percentStyle.Render(fmt.Sprintf("%.1f%%", opusPercent)),
			resetStr,
		)
		lines = append(lines, line)
	}

	return lines
}

// renderSessionMetricsFromOAuth renders session metrics from OAuth data (matching JSONL style)
func renderSessionMetricsFromOAuth(oauthData *oauth.UsageData, sessionDistribution string, barWidth int, now time.Time) []string {
	var lines []string

	resetTime, _ := oauth.ParseResetTime(oauthData.FiveHour.ResetsAt)

	// Check if the session has recently rolled over (ResetsAt is in the past)
	// When this happens, the Utilisation value may be stale (from the old session)
	sessionJustRolledOver := !resetTime.After(now)
	if sessionJustRolledOver {
		resetTime = resetTime.Add(5 * time.Hour)
	}

	percent := oauthData.FiveHour.Utilisation

	// If the session just rolled over and utilisation is suspiciously high for a new session,
	// the data is likely stale. Calculate expected max utilisation based on elapsed time.
	// A new session should have low utilisation proportional to time elapsed.
	if sessionJustRolledOver {
		// Calculate how long since the session started (time elapsed since ResetsAt - 5h)
		sessionStart := resetTime.Add(-5 * time.Hour)
		elapsed := now.Sub(sessionStart)

		// Maximum reasonable utilisation = (elapsed / 5 hours) * 100
		// e.g., 30 minutes into a 5-hour session = max 10% utilisation
		maxReasonablePercent := (elapsed.Hours() / 5.0) * 100
		if maxReasonablePercent < 1 {
			maxReasonablePercent = 1 // Floor at 1%
		}

		// If reported utilisation is much higher than possible, it's stale data
		if percent > maxReasonablePercent*2 {
			// Clear distribution since it's also from the old session
			sessionDistribution = ""
			percent = 0 // Show 0% for new session until API updates
		}
	}

	filled := min(int((percent/100)*float64(barWidth-2)), barWidth-2)
	bar := "[" + strings.Repeat("█", filled) + strings.Repeat("░", barWidth-2-filled) + "]"

	// Use same green-to-red colour for both bar and percentage
	usageStyle := GetPercentageStyle(percent)

	// Session usage with distribution on same line
	line := formatRow(
		"💸",
		"Session - Usage:",
		usageStyle.Render(bar),
		usageStyle.Render(fmt.Sprintf("%.1f%%", percent)),
		sessionDistribution,
	)
	lines = append(lines, line)

	timeUntilReset := time.Until(resetTime)
	totalSessionDuration := 5 * time.Hour
	remaining := timeUntilReset.Hours()
	if remaining < 0 {
		remaining = 0
	}

	// Calculate percentage remaining (starts at 100%, counts down to 0%)
	remainingPercent := (remaining / totalSessionDuration.Hours()) * 100
	if remainingPercent < 0 {
		remainingPercent = 0
	}
	if remainingPercent > 100 {
		remainingPercent = 100
	}

	// Build progress bar that empties from right to left as time runs out
	timeFilled := min(int((remainingPercent/100)*float64(barWidth-2)), barWidth-2)
	if timeFilled < 0 {
		timeFilled = 0
	}
	// Reverse: empty blocks on left, filled blocks on right (drains from right to left)
	timeBar := "[" + strings.Repeat("░", barWidth-2-timeFilled) + strings.Repeat("█", timeFilled) + "]"

	// For time remaining, use gold → green gradient (100% = gold/calm, 0% = green/ready to reset)
	timeStyle := GetTimeRemainingStyle(remainingPercent)

	timeLine := formatRow(
		"⏱️",
		"Time Before Reset:",
		timeStyle.Render(timeBar),
		timeStyle.Render(fmt.Sprintf("%.1f%%", remainingPercent)),
		timeStyle.Render(fmt.Sprintf("⏱️ Remaining: %.1f / %.1f hours", remaining, totalSessionDuration.Hours())),
	)
	lines = append(lines, timeLine)

	return lines
}

// renderPredictionWithOAuth renders prediction combining OAuth reset time with JSONL burn rate
func renderPredictionWithOAuth(oauthData *oauth.UsageData, session *models.SessionBlock, costBurnRate float64, limits models.Limits, now time.Time, showWeekly bool) string {
	resetTime, _ := oauth.ParseResetTime(oauthData.FiveHour.ResetsAt)

	// Check if the session has recently rolled over (ResetsAt is in the past)
	sessionJustRolledOver := !resetTime.After(now)
	if sessionJustRolledOver {
		resetTime = resetTime.Add(5 * time.Hour)
	}

	// Get utilisation, but check if it's stale after a session rollover
	utilisationPercent := oauthData.FiveHour.Utilisation
	if sessionJustRolledOver {
		sessionStart := resetTime.Add(-5 * time.Hour)
		elapsed := now.Sub(sessionStart)
		maxReasonablePercent := (elapsed.Hours() / 5.0) * 100
		if maxReasonablePercent < 1 {
			maxReasonablePercent = 1
		}
		if utilisationPercent > maxReasonablePercent*2 {
			utilisationPercent = 0 // Stale data, treat as new session
		}
	}

	resetTimeStr := resetTime.Local().Format("3:04 PM")
	whiteStyle := lipgloss.NewStyle().Foreground(ColorWhite)
	purpleStyle := lipgloss.NewStyle().Foreground(ColorPrediction)
	pinkStyle := lipgloss.NewStyle().Foreground(ColorOpus)

	var costDepletionStr string
	var costStyle lipgloss.Style
	hasCostPrediction := false

	// Predict session depletion from OAuth utilisation rate (session average).
	// This is more accurate than the CLI-derived cost burn rate because it uses
	// the same data source that determines the session limit, avoiding the
	// mismatch between instantaneous CLI burn rate and total (web + CLI) usage.
	if session != nil && session.IsActive {
		if utilisationPercent >= 100 {
			// Already at or over the session limit
			hasCostPrediction = true
			costDepletionStr = "NOW"
			costStyle = lipgloss.NewStyle().Foreground(ColorDanger)
		} else if utilisationPercent > 0 {
			// Derive burn rate from OAuth utilisation over elapsed session time
			sessionStart := resetTime.Add(-5 * time.Hour)
			elapsedMinutes := now.Sub(sessionStart).Minutes()

			if elapsedMinutes >= 1 {
				utilisationRate := utilisationPercent / elapsedMinutes // % per minute
				remainingPercent := 100.0 - utilisationPercent
				minutesToDepletion := remainingPercent / utilisationRate
				costDepletion := now.Add(time.Duration(minutesToDepletion * float64(time.Minute)))

				hasCostPrediction = true
				costDepletionStr = costDepletion.Local().Format("3:04 PM")

				// Colour based on whether depletion is before or after reset
				if costDepletion.Before(resetTime) {
					// Depletion BEFORE reset - we'll hit limit before resetting (BAD)
					timeUntilDepletion := costDepletion.Sub(now)
					if timeUntilDepletion <= 10*time.Minute {
						costStyle = lipgloss.NewStyle().Foreground(ColorDanger)
					} else if timeUntilDepletion <= 30*time.Minute {
						costStyle = lipgloss.NewStyle().Foreground(ColorPrimary)
					} else {
						costStyle = lipgloss.NewStyle().Foreground(ColorWarning)
					}
				} else {
					// Depletion after reset - limits will reset before we hit them
					timeAfterReset := costDepletion.Sub(resetTime)
					if timeAfterReset <= 30*time.Minute {
						// Close enough to reset that it's still worth showing
						costStyle = lipgloss.NewStyle().Foreground(ColorPrimary)
					} else {
						// Well after reset - prediction is meaningless, don't show it
						hasCostPrediction = false
					}
				}
			}
		}
	}

	// Build session prediction part
	var sessionPart string
	if hasCostPrediction {
		sessionPart = fmt.Sprintf("[%s]",
			costStyle.Render(fmt.Sprintf("Session limit: %s", costDepletionStr)))
	} else {
		sessionPart = fmt.Sprintf("[%s]",
			whiteStyle.Render(fmt.Sprintf("Resets: %s", resetTimeStr)))
	}

	// Build weekly prediction part - only show if there's a problem (not OK)
	var weeklyPart string
	if showWeekly {
		weeklyPrediction := analysis.PredictWeeklyDepletion(oauthData, costBurnRate, limits.CostLimitUSD, now)
		if !weeklyPrediction.ResetTime.IsZero() {
			var weeklyStr string
			var weeklyStyle lipgloss.Style
			showWeeklyPart := false // Only show if there's an issue

			if weeklyPrediction.Utilisation >= 100 {
				weeklyStr = "Weekly limit exceeded!"
				weeklyStyle = lipgloss.NewStyle().Foreground(ColorDanger)
				showWeeklyPart = true
			} else if !weeklyPrediction.DepletionTime.IsZero() && weeklyPrediction.WillHitLimit {
				// Will hit limit before reset - show when
				timeUntil := weeklyPrediction.DepletionTime.Sub(now)
				weeklyDepletionStr := fmt.Sprintf("%s %s",
					weeklyPrediction.DepletionTime.Local().Format("Mon"),
					weeklyPrediction.DepletionTime.Local().Format("3:04 PM"))

				switch {
				case timeUntil <= 6*time.Hour:
					weeklyStyle = lipgloss.NewStyle().Foreground(ColorDanger) // Red
				case timeUntil <= 12*time.Hour:
					weeklyStyle = lipgloss.NewStyle().Foreground(ColorPrimary) // Orange
				default:
					// More than 12h but will still hit limit before reset - yellow
					weeklyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD700"))
				}

				weeklyStr = fmt.Sprintf("Weekly limit: %s", weeklyDepletionStr)
				showWeeklyPart = true
			}
			// Skip showing "Weekly: OK" or "Insufficient data" - only show problems

			if showWeeklyPart {
				weeklyPart = " | [" + weeklyStyle.Render(weeklyStr) + "]"
			}
		}
	}

	// Check for unused utilisation warning
	reminder := ""
	timeUntilReset := resetTime.Sub(now)
	if timeUntilReset > 0 && timeUntilReset < time.Hour && utilisationPercent < 75 {
		reminder = " | " + pinkStyle.Render("✈️  Unused session utilisation expiring soon")
	}

	// Build "Updated:" timestamp
	updatedStr := ""
	if !oauthData.FetchedAt.IsZero() {
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))
		updatedStr = " | " + dimStyle.Render(fmt.Sprintf("Updated: %s", oauthData.FetchedAt.Local().Format("3:04 PM")))
	}

	return fmt.Sprintf("🔮 %s %s%s%s%s",
		purpleStyle.Render("Prediction:"),
		sessionPart,
		weeklyPart,
		reminder,
		updatedStr,
	)
}

// renderOAuthLimitWarning renders warning if OAuth limits are approaching
func renderOAuthLimitWarning(oauthData *oauth.UsageData, now time.Time) string {
	percent := oauthData.FiveHour.Utilisation

	// Check if the session just rolled over - if so, high utilisation is likely stale
	resetTime, _ := oauth.ParseResetTime(oauthData.FiveHour.ResetsAt)
	if !resetTime.After(now) {
		// Session rolled over - check if utilisation is plausible
		sessionStart := resetTime
		elapsed := now.Sub(sessionStart)
		maxReasonablePercent := (elapsed.Hours() / 5.0) * 100
		if maxReasonablePercent < 1 {
			maxReasonablePercent = 1
		}

		// If utilisation is much higher than possible, it's stale - don't warn
		if percent > maxReasonablePercent*2 {
			return ""
		}
	}

	if percent > 95 {
		warningText := fmt.Sprintf("🚨 CRITICAL: Session usage at %.1f%%!", percent)
		return CriticalStyle.Render(warningText)
	} else if percent > 85 {
		warningText := fmt.Sprintf("⚠️  WARNING: Session usage at %.1f%%", percent)
		return WarningStyle.Render(warningText)
	}

	return ""
}

// calculateCostBurnRateFromSessions calculates cost burn rate using proportional overlap (like CalculateBurnRate but for cost)
func calculateCostBurnRateFromSessions(blocks []models.SessionBlock, currentTime time.Time) float64 {
	oneHourAgo := currentTime.Add(-1 * time.Hour)
	totalCost := 0.0

	for _, block := range blocks {
		if block.IsGap {
			continue
		}

		// Determine actual end time
		sessionEnd := currentTime
		if !block.IsActive && block.ActualEndTime != nil {
			sessionEnd = *block.ActualEndTime
		} else if !block.IsActive {
			sessionEnd = block.EndTime
		}

		// Check if session overlaps with last hour
		if sessionEnd.Before(oneHourAgo) {
			continue // Session ended before the hour window
		}

		if block.StartTime.After(currentTime) {
			continue // Session hasn't started yet
		}

		// Calculate overlap period
		sessionStartInHour := block.StartTime
		if oneHourAgo.After(sessionStartInHour) {
			sessionStartInHour = oneHourAgo
		}

		sessionEndInHour := sessionEnd
		if currentTime.Before(sessionEndInHour) {
			sessionEndInHour = currentTime
		}

		if sessionEndInHour.Before(sessionStartInHour) {
			continue // No overlap
		}

		// Calculate proportion of session in the hour window
		totalSessionDuration := sessionEnd.Sub(block.StartTime).Minutes()
		hourDuration := sessionEndInHour.Sub(sessionStartInHour).Minutes()

		if totalSessionDuration > 0 {
			proportion := hourDuration / totalSessionDuration
			costInHour := block.CostUSD * proportion
			totalCost += costInHour
		}
	}

	// Divide by 60 to get cost per minute (matching token burn rate calculation)
	return totalCost / 60.0
}
