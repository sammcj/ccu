package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ============================================================================
// COLOUR CONFIGURATION - Edit these values to customise CCU's appearance
// ============================================================================
//
// This section contains ALL colour definitions used throughout CCU.
// Each colour includes documentation showing exactly where it's used.

// Base UI Colours
// These are used for general UI elements, warnings, and text
var (
	ColorPrimary    = lipgloss.Color("#FF8C42") // Neon orange - Used for: burn rate bars when near limit
	ColorSuccess    = lipgloss.Color("#00FF88") // Neon green - Used for: low usage indicators, success messages
	ColorWarning    = lipgloss.Color("#FF10F0") // Neon magenta - Used for: warning messages
	ColorDanger     = lipgloss.Color("#FF006E") // Neon pink - Used for: critical warnings, cost depletion before reset
	ColorInfo       = lipgloss.Color("#00D9FF") // Neon blue - Used for: informational text
	ColorMuted      = lipgloss.Color("#8B7FA8") // Muted purple - Used for: subtle UI elements
	ColorText       = lipgloss.Color("#E0E0FF") // Light purple-white - Used for: general text
	ColorWhite      = lipgloss.Color("#FFFFFF") // Bright white - Used for: "Resets at:" text, model percentages
	ColorBackground = lipgloss.Color("#1A1A2E") // Dark blue-black - Used for: background (when applicable)
)

// Model Name Colours
// Used for colour-coding model names in session distribution display
var (
	ColorSonnet = lipgloss.Color("#0088FF") // Blue - Used for: "Sonnet 4.5", "Sonnet 4", "Sonnet 3.5" model names
	ColorOpus   = lipgloss.Color("#FFB3D9") // Mellow pink - Used for: "Opus 4.5", "Opus 4", "Opus 3" model names
	ColorHaiku  = lipgloss.Color("#9B72CF") // Violet - Used for: "Haiku 4.5", "Haiku 3.5", "Haiku 3" model names
)

// UI Element Colours
// Specific colours for individual UI components
var (
	ColorPrediction = lipgloss.Color("#9D4EDD") // Purple - Used for: "Prediction:" label text
)

// Usage Percentage Gradients (Green → Red)
// These create smooth colour transitions based on percentage values
// Used for: Weekly usage bars, Session usage bars, Burn rate bars, Percentage values, Hours used
var (
	ColorPercent0   = lipgloss.Color("#00FF88") // Bright green (0-10%)
	ColorPercent10  = lipgloss.Color("#40FF70") // Light green (10-20%)
	ColorPercent20  = lipgloss.Color("#80FF58") // Yellow-green (20-30%)
	ColorPercent30  = lipgloss.Color("#B0FF40") // Lime (30-40%)
	ColorPercent40  = lipgloss.Color("#E0FF28") // Yellow-lime (40-50%)
	ColorPercent50  = lipgloss.Color("#FFDD00") // Yellow (50-60%)
	ColorPercent60  = lipgloss.Color("#FFB000") // Orange (60-70%)
	ColorPercent70  = lipgloss.Color("#FF8800") // Orange-red (70-80%)
	ColorPercent80  = lipgloss.Color("#FF5500") // Red-orange (80-90%)
	ColorPercent90  = lipgloss.Color("#FF2200") // Red (90-100%)
	ColorPercent100 = lipgloss.Color("#FF0000") // Bright red (100%)
)

// Time Remaining Gradient (Gold → Green)
// Creates a countdown colour scheme where more time = gold, less time = green
// Used for: "Time Before Reset" bar and percentage, "Remaining: X / 5.0 hours" text
var (
	ColorTimeRemaining90 = lipgloss.Color("#FFD700") // Gold (90-100%)
	ColorTimeRemaining80 = lipgloss.Color("#FFC700") // Slightly darker gold (80-90%)
	ColorTimeRemaining70 = lipgloss.Color("#FFB700") // Orange-gold (70-80%)
	ColorTimeRemaining60 = lipgloss.Color("#FFA700") // Orange-yellow (60-70%)
	ColorTimeRemaining50 = lipgloss.Color("#FF9700") // Orange (50-60%)
	ColorTimeRemaining40 = lipgloss.Color("#D0B000") // Yellow-orange (40-50%)
	ColorTimeRemaining30 = lipgloss.Color("#A0C000") // Yellow-green (30-40%)
	ColorTimeRemaining20 = lipgloss.Color("#70D000") // Lime (20-30%)
	ColorTimeRemaining10 = lipgloss.Color("#40E040") // Light green (10-20%)
	ColorTimeRemaining0  = ColorSuccess              // Bright green (0-10%, about to reset)
)

// Progress Bar Gradient (Blue → Purple → Red) - Currently unused but kept for future features
var (
	ColorProgress0  = lipgloss.Color("#00D9FF") // Neon blue (0-10%)
	ColorProgress10 = lipgloss.Color("#00E5FF") // Light neon blue (10-20%)
	ColorProgress20 = lipgloss.Color("#00F0FF") // Cyan (20-30%)
	ColorProgress30 = lipgloss.Color("#40D5FF") // Sky blue (30-40%)
	ColorProgress40 = lipgloss.Color("#80BFFF") // Light purple-blue (40-50%)
	ColorProgress50 = lipgloss.Color("#B57EDC") // Neon purple (50-60%)
	ColorProgress60 = lipgloss.Color("#C570D4") // Purple (60-70%)
	ColorProgress70 = lipgloss.Color("#D560CB") // Purple-magenta (70-80%)
	ColorProgress80 = lipgloss.Color("#E550C0") // Magenta (80-90%)
	ColorProgress90 = lipgloss.Color("#FF40B0") // Pink-red (90-95%)
	ColorProgress95 = lipgloss.Color("#FF2090") // Neon red (95-100%)
)

// ============================================================================
// ELEMENT-TO-COLOUR MAPPINGS
// ============================================================================
//
// This section documents exactly which UI elements use which colours:
//
// WEEKLY USAGE (OAuth or JSONL):
//   - "Weekly - Sonnet:" bar          → ColorPercent* (based on percentage)
//   - "Weekly - Sonnet:" percentage   → ColorPercent* (based on percentage)
//   - "Weekly - Sonnet:" hours value  → ColorPercent* (based on percentage)
//   - "[Resets: Mon 6:00 AM]"         → ColorWhite
//   - Same applies for "Weekly - Opus:"
//
// BURN RATE:
//   - "Burn Rate - Tokens:" bar       → ColorPercent* (based on burn rate %)
//   - "Burn Rate - Tokens:" value     → ColorPercent* (based on burn rate %)
//   - "Burn Rate - Cost:" bar         → ColorPercent* (based on burn rate %)
//   - "Burn Rate - Cost:" value       → ColorPercent* (based on burn rate %)
//
// SESSION USAGE:
//   - "Session - Usage:" bar          → ColorPercent* (based on percentage)
//   - "Session - Usage:" percentage   → ColorPercent* (based on percentage)
//   - Model names in distribution     → ColorSonnet / ColorOpus / ColorHaiku
//   - Model percentage values         → ColorWhite
//
// TIME REMAINING:
//   - "Time Before Reset" bar         → ColorTimeRemaining* (based on % remaining)
//   - "Time Before Reset" percentage  → ColorTimeRemaining* (based on % remaining)
//   - "Remaining: X / 5.0 hours"      → ColorTimeRemaining* (based on % remaining)
//
// PREDICTION:
//   - "Prediction:" label             → ColorPrediction
//   - "Session limit: X:XX PM"      → ColorDanger (before reset) / ColorPrimary (close) / ColorSuccess (safe)
//   - "Resets at: X:XX PM"            → ColorWhite
//
// ============================================================================

// Styles
var (
	// Title style
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary).
			MarginBottom(1)

	// Section header style
	HeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorInfo).
			MarginTop(0).
			MarginBottom(0)

	// Label style
	LabelStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	// Bright white label style
	BrightLabelStyle = lipgloss.NewStyle().
				Foreground(ColorWhite)

	// Value style
	ValueStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorText)

	// Success style
	SuccessStyle = lipgloss.NewStyle().
			Foreground(ColorSuccess)

	// Warning style
	WarningStyle = lipgloss.NewStyle().
			Foreground(ColorWarning).
			Bold(true)

	// Danger style
	DangerStyle = lipgloss.NewStyle().
			Foreground(ColorDanger)

	// Critical style (for urgent warnings)
	CriticalStyle = lipgloss.NewStyle().
			Foreground(ColorDanger).
			Bold(true).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorDanger).
			Padding(0, 1)

	// Info style
	InfoStyle = lipgloss.NewStyle().
			Foreground(ColorInfo)

	// Box style for containers
	BoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorMuted).
			Padding(0, 1)

	// Status line style
	StatusStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			MarginTop(1)

	// Help style
	HelpStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Italic(true)
)

// GetProgressColour returns the colour for a progress percentage with gradient from blue → purple → red
func GetProgressColour(percent float64) lipgloss.Color {
	switch {
	case percent >= 95:
		return ColorProgress95 // Neon red
	case percent >= 90:
		return ColorProgress90 // Pink-red
	case percent >= 80:
		return ColorProgress80 // Magenta
	case percent >= 70:
		return ColorProgress70 // Purple-magenta
	case percent >= 60:
		return ColorProgress60 // Purple
	case percent >= 50:
		return ColorProgress50 // Neon purple
	case percent >= 40:
		return ColorProgress40 // Light purple-blue
	case percent >= 30:
		return ColorProgress30 // Sky blue
	case percent >= 20:
		return ColorProgress20 // Cyan
	case percent >= 10:
		return ColorProgress10 // Light neon blue
	default:
		return ColorProgress0 // Neon blue
	}
}

// GetProgressStyle returns a style for progress bars based on percentage
func GetProgressStyle(percent float64) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(GetProgressColour(percent))
}

// GetInverseProgressColour returns the colour for an inverse progress percentage (red → blue)
// Used for time elapsed where red at start (0%) gradually becomes blue at end (100%)
func GetInverseProgressColour(percent float64) lipgloss.Color {
	// Invert the percentage for color lookup
	invertedPercent := 100 - percent

	switch {
	case invertedPercent >= 95:
		return ColorProgress95 // Neon red at 0-5% elapsed
	case invertedPercent >= 90:
		return ColorProgress90 // Pink-red at 5-10% elapsed
	case invertedPercent >= 80:
		return ColorProgress80 // Magenta at 10-20% elapsed
	case invertedPercent >= 70:
		return ColorProgress70 // Purple-magenta at 20-30% elapsed
	case invertedPercent >= 60:
		return ColorProgress60 // Purple at 30-40% elapsed
	case invertedPercent >= 50:
		return ColorProgress50 // Neon purple at 40-50% elapsed
	case invertedPercent >= 40:
		return ColorProgress40 // Light purple-blue at 50-60% elapsed
	case invertedPercent >= 30:
		return ColorProgress30 // Sky blue at 60-70% elapsed
	case invertedPercent >= 20:
		return ColorProgress20 // Cyan at 70-80% elapsed
	case invertedPercent >= 10:
		return ColorProgress10 // Light neon blue at 80-90% elapsed
	default:
		return ColorProgress0 // Neon blue at 90-100% elapsed
	}
}

// GetInverseProgressStyle returns a style for inverse progress bars
func GetInverseProgressStyle(percent float64) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(GetInverseProgressColour(percent))
}

// GetPercentageColour returns the colour for percentage text (green to red)
func GetPercentageColour(percent float64) lipgloss.Color {
	switch {
	case percent >= 100:
		return ColorPercent100 // Bright red at 100%+
	case percent >= 90:
		return ColorPercent90 // Red
	case percent >= 80:
		return ColorPercent80 // Red-orange
	case percent >= 70:
		return ColorPercent70 // Orange-red
	case percent >= 60:
		return ColorPercent60 // Orange
	case percent >= 50:
		return ColorPercent50 // Yellow
	case percent >= 40:
		return ColorPercent40 // Yellow-lime
	case percent >= 30:
		return ColorPercent30 // Lime
	case percent >= 20:
		return ColorPercent20 // Yellow-green
	case percent >= 10:
		return ColorPercent10 // Light green
	default:
		return ColorPercent0 // Bright green (0-10%)
	}
}

// GetPercentageStyle returns a style for percentage text
func GetPercentageStyle(percent float64) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(GetPercentageColour(percent))
}

// GetTimeRemainingColour returns the colour for time remaining bars (gold → green)
// High percentage (lots of time) = gold/yellow, low percentage (approaching reset) = green
func GetTimeRemainingColour(percent float64) lipgloss.Color {
	switch {
	case percent >= 90:
		return ColorTimeRemaining90
	case percent >= 80:
		return ColorTimeRemaining80
	case percent >= 70:
		return ColorTimeRemaining70
	case percent >= 60:
		return ColorTimeRemaining60
	case percent >= 50:
		return ColorTimeRemaining50
	case percent >= 40:
		return ColorTimeRemaining40
	case percent >= 30:
		return ColorTimeRemaining30
	case percent >= 20:
		return ColorTimeRemaining20
	case percent >= 10:
		return ColorTimeRemaining10
	default:
		return ColorTimeRemaining0
	}
}

// GetTimeRemainingStyle returns a style for time remaining displays
func GetTimeRemainingStyle(percent float64) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(GetTimeRemainingColour(percent))
}

// GetModelColour returns the colour for a model name
func GetModelColour(modelName string) lipgloss.Color {
	lowerModel := strings.ToLower(modelName)
	if strings.Contains(lowerModel, "sonnet") {
		return ColorSonnet
	} else if strings.Contains(lowerModel, "haiku") {
		return ColorHaiku
	} else if strings.Contains(lowerModel, "opus") {
		return ColorOpus
	}
	return ColorWhite // Default white for unknown models
}

// GetModelStyle returns a style for model names
func GetModelStyle(modelName string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(GetModelColour(modelName))
}
