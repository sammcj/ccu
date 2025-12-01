package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sammcj/ccu/internal/models"
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
	refreshRate := flag.Int("refresh", 30, "Refresh rate in seconds (1-60, default 30 for JSONL, 60 for OAuth)")
	hoursBack := flag.Int("hours", 24, "Hours of history to load")
	dataPath := flag.String("data", "", "Path to Claude data directory (default: ~/.claude/projects)")
	customTokens := flag.Int("custom-tokens", 0, "Custom token limit (requires -plan=custom)")
	customCost := flag.Float64("custom-cost", 0, "Custom cost limit USD (requires -plan=custom)")
	customMessages := flag.Int("custom-messages", 0, "Custom message limit (requires -plan=custom)")
	showWeekly := flag.Bool("weekly", true, "Show weekly usage panel")
	showHelp := flag.Bool("help", false, "Show help message")
	showVersion := flag.Bool("version", false, "Show version information")

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

	// Validate and set refresh rate
	if *refreshRate < 1 || *refreshRate > 60 {
		return nil, fmt.Errorf("refresh rate must be between 1 and 60 seconds")
	}
	config.RefreshRate = time.Duration(*refreshRate) * time.Second

	// Set hours back
	if *hoursBack < 1 {
		return nil, fmt.Errorf("hours must be at least 1")
	}
	config.HoursBack = *hoursBack

	// Set weekly flag
	config.ShowWeekly = *showWeekly

	return config, nil
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
	fmt.Println("  ccu -view=daily                        # Show daily aggregation view")
	fmt.Println("  ccu -refresh=10                        # Refresh every 10 seconds")
	fmt.Println("  ccu -hours=48                          # Load last 48 hours of data")
	fmt.Println("  ccu -plan=custom -custom-tokens=50000  # Use custom token limit")
	fmt.Println()
}

// printVersion prints version information
func printVersion() {
	fmt.Printf("ccu version %s\n", Version)
	fmt.Printf("Commit: %s\n", Commit)
	fmt.Printf("Build Date: %s\n", BuildDate)
}
