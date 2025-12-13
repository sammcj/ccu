package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sammcj/ccu/internal/app"
	"github.com/sammcj/ccu/internal/config"
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

	// Create application model
	model := app.NewModel(cfg)

	// Create Bubbletea program
	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),       // Use alternate screen buffer
		tea.WithMouseCellMotion(), // Enable mouse support
		tea.WithReportFocus(),     // Detect terminal focus for refresh on wake
	)

	// Run the program
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running application: %v\n", err)
		os.Exit(1)
	}
}
