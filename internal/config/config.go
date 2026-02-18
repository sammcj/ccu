package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sammcj/ccu/internal/models"
	"github.com/sammcj/ccu/internal/oauth"
)

// Version information (set by main package)
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

// ParseFlags parses command-line flags and returns a config
func ParseFlags() (*models.Config, error) {
	config := models.DefaultConfig()

	// Define flags
	plan := flag.String("plan", "max5", "Plan type: pro, max5, max20, custom")
	viewMode := flag.String("view", "realtime", "View mode: realtime, daily, monthly")
	reportMode := flag.String("report", "", "Generate static report to stdout: daily, weekly, monthly (bypasses TUI)")
	refreshRate := flag.Int("refresh", 30, "Refresh rate in seconds (1-60, default 30 for JSONL, 60 for OAuth)")
	hoursBack := flag.Int("hours", 24, "Hours of history to load")
	dataPath := flag.String("data", "", "Path to Claude data directory (default: ~/.claude/projects)")
	customTokens := flag.Int("custom-tokens", 0, "Custom token limit (requires -plan=custom)")
	customCost := flag.Float64("custom-cost", 0, "Custom cost limit USD (requires -plan=custom)")
	customMessages := flag.Int("custom-messages", 0, "Custom message limit (requires -plan=custom)")
	showWeekly := flag.Bool("weekly", true, "Show weekly usage panel")
	showHelp := flag.Bool("help", false, "Show help message")
	showVersion := flag.Bool("version", false, "Show version information")

	// API server flags
	apiEnabled := flag.Bool("api", false, "Enable embedded HTTP API server")
	apiPort := flag.Int("api-port", 19840, "API server listen port")
	apiBind := flag.String("api-bind", "0.0.0.0", "API server bind address")
	apiToken := flag.String("api-token", "", "API bearer token (empty = no auth)")
	apiAllow := flag.String("api-allow", "", "Comma-separated CIDR ranges to allowlist (empty = allow all)")

	flag.Parse()

	// Handle help
	if *showHelp {
		printHelp()
		os.Exit(0)
	}

	// Handle version
	if *showVersion {
		printVersion()
		os.Exit(0)
	}

	// Set data path
	if *dataPath != "" {
		config.DataPath = *dataPath
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("getting home directory: %w", err)
		}
		config.DataPath = filepath.Join(homeDir, ".claude", "projects")
	}

	// Auto-detect plan from keychain when the user hasn't set it explicitly.
	// flag.Visit only visits flags that were explicitly provided on the command line.
	planExplicit := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "plan" {
			planExplicit = true
		}
	})
	if !planExplicit {
		if detected := oauth.DetectPlan(); detected != "" {
			*plan = detected
		}
	}

	// Validate and set plan
	switch *plan {
	case "pro", "max5", "max20", "custom":
		config.Plan = *plan
	default:
		return nil, fmt.Errorf("invalid plan: %s (must be pro, max5, max20, or custom)", *plan)
	}

	// Set custom limits if plan is custom
	if *plan == "custom" {
		if *customTokens > 0 {
			config.CustomToken = *customTokens
		}
		if *customCost > 0 {
			config.CustomCost = *customCost
		}
		if *customMessages > 0 {
			config.CustomMessages = *customMessages
		}
	}

	// Validate and set view mode
	switch *viewMode {
	case "realtime", "daily", "monthly":
		config.ViewMode = models.ViewMode(*viewMode)
	default:
		return nil, fmt.Errorf("invalid view mode: %s (must be realtime, daily, or monthly)", *viewMode)
	}

	// Validate and set report mode
	switch *reportMode {
	case "":
		config.ReportMode = models.ReportModeNone
	case "daily":
		config.ReportMode = models.ReportModeDaily
	case "weekly":
		config.ReportMode = models.ReportModeWeekly
	case "monthly":
		config.ReportMode = models.ReportModeMonthly
	default:
		return nil, fmt.Errorf("invalid report mode: %s (must be daily, weekly, or monthly)", *reportMode)
	}

	// Validate and set refresh rate
	if *refreshRate < 1 || *refreshRate > 60 {
		return nil, fmt.Errorf("refresh rate must be between 1 and 60 seconds")
	}
	config.RefreshRate = time.Duration(*refreshRate) * time.Second

	// Set hours back - auto-adjust for report modes if using default
	if *hoursBack < 1 {
		return nil, fmt.Errorf("hours must be at least 1")
	}
	config.HoursBack = *hoursBack

	// For report modes, auto-expand hours to capture relevant data
	if config.ReportMode != models.ReportModeNone && *hoursBack == 24 {
		switch config.ReportMode {
		case models.ReportModeDaily:
			config.HoursBack = 720 // 30 days
		case models.ReportModeWeekly:
			config.HoursBack = 2160 // 90 days (~13 weeks)
		case models.ReportModeMonthly:
			config.HoursBack = 8760 // 365 days (1 year)
		}
	}

	// Set weekly flag
	config.ShowWeekly = *showWeekly

	// API server configuration (precedence: CLI flag > env var > token file)
	config.API.Enabled = *apiEnabled
	if !config.API.Enabled {
		if v := os.Getenv("CCU_API"); v == "true" || v == "1" {
		config.API.Enabled = true
	} else if v := os.Getenv("CCU_ENABLE_API"); v == "true" || v == "1" {
			config.API.Enabled = true
		}
	}

	if *apiPort != 19840 {
		config.API.Port = *apiPort
	} else if v := os.Getenv("CCU_API_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			config.API.Port = p
		}
	} else {
		config.API.Port = *apiPort
	}

	if *apiBind != "0.0.0.0" {
		config.API.BindAddr = *apiBind
	} else if v := os.Getenv("CCU_API_BIND"); v != "" {
		config.API.BindAddr = v
	} else {
		config.API.BindAddr = *apiBind
	}

	if *apiToken != "" {
		config.API.Token = *apiToken
	} else if v := os.Getenv("CCU_API_TOKEN"); v != "" {
		config.API.Token = v
	} else {
		// Token file fallback
		config.API.Token = readTokenFile()
	}

	if *apiAllow != "" {
		config.API.AllowedCIDRs = parseCIDRList(*apiAllow)
	} else if v := os.Getenv("CCU_API_ALLOW"); v != "" {
		config.API.AllowedCIDRs = parseCIDRList(v)
	}

	return config, nil
}

// readTokenFile reads a bearer token from $HOME/.ccu/.api_token if present.
func readTokenFile() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(homeDir, ".ccu", ".api_token"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// parseCIDRList splits a comma-separated string of CIDR ranges into a slice.
func parseCIDRList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// printHelp prints help message
func printHelp() {
	fmt.Println("ccu - Claude Code Usage Monitor")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  ccu [flags]")
	fmt.Println()
	fmt.Println("Flags:")
	flag.PrintDefaults()
	fmt.Println()
	fmt.Println("Data Sources:")
	fmt.Println("  OAuth API (preferred): Live usage from Anthropic servers (60s default refresh)")
	fmt.Println("  JSONL files (fallback): Local CLI activity parsing (30s default refresh)")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  ccu                                    # Run with default settings (Max5 plan)")
	fmt.Println("  ccu -plan=pro                          # Use Pro plan limits")
	fmt.Println("  ccu -view=daily                        # Show daily aggregation view (TUI)")
	fmt.Println("  ccu -report=monthly                    # Print monthly usage report to stdout")
	fmt.Println("  ccu -report=weekly                     # Print weekly usage report to stdout")
	fmt.Println("  ccu -report=daily -hours=90           # Print last 90 days of daily usage")
	fmt.Println("  ccu -refresh=10                        # Refresh every 10 seconds")
	fmt.Println("  ccu -hours=48                          # Load last 48 hours of data")
	fmt.Println("  ccu -plan=custom -custom-tokens=50000  # Use custom token limit")
	fmt.Println("  ccu -api -api-port=19840               # Enable HTTP API server")
	fmt.Println("  ccu -api -api-token=secret             # API server with bearer token auth")
	fmt.Println("  ccu -api -api-allow=192.168.1.0/24     # API server with IP allowlist")
	fmt.Println()
}

// printVersion prints version information
func printVersion() {
	fmt.Printf("ccu version %s\n", Version)
	fmt.Printf("Commit: %s\n", Commit)
	fmt.Printf("Build Date: %s\n", BuildDate)
}
