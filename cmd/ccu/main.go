package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sammcj/ccu/internal/app"
	"github.com/sammcj/ccu/internal/config"
	"github.com/sammcj/ccu/internal/data"
	"github.com/sammcj/ccu/internal/models"
	"github.com/sammcj/ccu/internal/ui"
)

// Version information, set via ldflags during build
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func main() {
	// Set version info in config package for --version flag
	config.Version = Version
	config.Commit = Commit
	config.BuildDate = BuildDate

	// Parse configuration
	cfg, err := config.ParseFlags()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Handle report mode (non-interactive output to stdout)
	if cfg.ReportMode != models.ReportModeNone {
		runReport(cfg)
		return
	}

	// Create application model
	model := app.NewModel(cfg)

	// Create Bubbletea program
	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),     // Use alternate screen buffer
		tea.WithReportFocus(),   // Get focus/blur events (triggers refresh on terminal focus)
	)

	// Listen for SIGCONT (process resumed after suspension/sleep) and inject
	// a resume message into the Bubbletea program to force a data refresh
	sigCont := make(chan os.Signal, 1)
	signal.Notify(sigCont, syscall.SIGCONT)
	go func() {
		for range sigCont {
			p.Send(app.ResumeMsg())
		}
	}()

	// Run the program
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running application: %v\n", err)
		os.Exit(1)
	}
}

// runReport generates a static report and outputs to stdout
func runReport(cfg *models.Config) {
	// Load usage data
	entries, err := data.LoadUsageData(cfg.DataPath, cfg.HoursBack)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading data: %v\n", err)
		os.Exit(1)
	}

	if len(entries) == 0 {
		fmt.Println("No usage data found.")
		return
	}

	// Generate report based on mode
	var report string
	switch cfg.ReportMode {
	case models.ReportModeDaily:
		report = ui.GenerateDailyReport(entries, cfg.Timezone)
	case models.ReportModeWeekly:
		report = ui.GenerateWeeklyReport(entries, cfg.Timezone)
	case models.ReportModeMonthly:
		report = ui.GenerateMonthlyReport(entries, cfg.Timezone)
	}

	fmt.Print(report)
}
