package models

import "time"

// APIConfig holds configuration for the optional embedded HTTP API server
type APIConfig struct {
	Enabled      bool
	Port         int      // default 19840
	BindAddr     string   // default "0.0.0.0"
	Token        string   // shared secret; empty = no auth
	AllowedCIDRs []string // e.g. ["192.168.0.0/24", "10.0.0.1/32"]; empty = allow all
}

// Config holds application configuration
type Config struct {
	// Data paths
	DataPath string

	// Plan configuration
	Plan           string
	CustomToken    int
	CustomCost     float64
	CustomMessages int

	// Display configuration
	ViewMode      ViewMode
	RefreshRate   time.Duration
	Timezone      *time.Location
	Theme         Theme
	HoursBack     int

	// Feature flags
	ShowWeekly bool

	// Report mode (non-interactive output to stdout)
	ReportMode ReportMode

	// API server configuration
	API APIConfig
}

// ViewMode represents the display mode
type ViewMode string

const (
	ViewModeRealtime ViewMode = "realtime"
	ViewModeDaily    ViewMode = "daily"
	ViewModeMonthly  ViewMode = "monthly"
)

// Theme represents the colour theme
type Theme string

const (
	ThemeAuto  Theme = "auto"
	ThemeLight Theme = "light"
	ThemeDark  Theme = "dark"
)

// ReportMode represents non-interactive report output
type ReportMode string

const (
	ReportModeNone    ReportMode = ""
	ReportModeDaily   ReportMode = "daily"
	ReportModeWeekly  ReportMode = "weekly"
	ReportModeMonthly ReportMode = "monthly"
)

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		DataPath:       "",  // Will be set to ~/.claude/projects
		Plan:           "max5",
		ViewMode:       ViewModeRealtime,
		RefreshRate:    30 * time.Second, // Default for JSONL mode, adjusted to 60s for OAuth
		Timezone:       time.Local,
		Theme:          ThemeAuto,
		HoursBack:      24,
		ShowWeekly:     true, // Shows estimated weekly usage based on recent activity
		CustomToken:    0,
		CustomCost:     0,
		CustomMessages: 0,
		API: APIConfig{
			Port:     19840,
			BindAddr: "0.0.0.0",
		},
	}
}

// GetEffectiveLimits returns the limits based on config
func (c *Config) GetEffectiveLimits() Limits {
	if c.Plan == "custom" && c.CustomToken > 0 {
		return Limits{
			PlanName:     "Custom",
			TokenLimit:   c.CustomToken,
			CostLimitUSD: c.CustomCost,
			MessageLimit: c.CustomMessages,
		}
	}
	return GetLimits(c.Plan)
}
