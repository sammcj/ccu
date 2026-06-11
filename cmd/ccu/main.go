package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sammcj/ccu/internal/api"
	"github.com/sammcj/ccu/internal/app"
	"github.com/sammcj/ccu/internal/config"
	"github.com/sammcj/ccu/internal/data"
	"github.com/sammcj/ccu/internal/modelcheck"
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

	// Handle model table check (non-interactive, exits with status code)
	if cfg.CheckModels {
		os.Exit(runModelCheck())
	}

	// Handle report mode (non-interactive output to stdout)
	if cfg.ReportMode != models.ReportModeNone {
		runReport(cfg)
		return
	}

	// Create application model
	model := app.NewModel(cfg)

	// Start optional API server
	if cfg.API.Enabled {
		apiServer := api.New(cfg.API)
		model.SetAPIServer(apiServer)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			if err := apiServer.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
				fmt.Fprintf(os.Stderr, "API server error: %v\n", err)
			}
		}()
	}

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

// runModelCheck compares ccu's model tables against upstream pricing.
// Returns 0 when in sync, 1 when drift was found, 2 on fetch/parse errors.
func runModelCheck() int {
	data, err := modelcheck.FetchUpstream(context.Background(), modelcheck.UpstreamURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching upstream pricing: %v\n", err)
		return 2
	}

	report, err := modelcheck.Compare(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error comparing model tables: %v\n", err)
		return 2
	}

	fmt.Print(report.Format())
	if len(report.Findings) > 0 {
		return 1
	}
	return 0
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
