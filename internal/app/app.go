package app

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/sammcj/ccu/internal/analysis"
	"github.com/sammcj/ccu/internal/data"
	"github.com/sammcj/ccu/internal/models"
	"github.com/sammcj/ccu/internal/oauth"
	"github.com/sammcj/ccu/internal/ui"
)

func init() {
	// Send logs to stderr so they don't interfere with TUI
	log.SetOutput(os.Stderr)
	// Optionally disable logs entirely in production:
	// log.SetOutput(io.Discard)
	_ = io.Discard // silence unused import warning
}

// Messages
type tickMsg time.Time
type dataLoadedMsg struct {
	entries       []models.UsageEntry
	oauthData     *oauth.UsageData
	err           error
	oauthErr      error // Separate OAuth error for proper handling
	oauthDisabled bool  // Whether OAuth should be permanently disabled
}
type clearScreenMsg struct{}

// Init initialises the application
func (m AppModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		loadDataCmd(m.config),
		tickCmd(m.config.RefreshRate),
	)
}

// Update handles messages and updates the model
func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.SetDimensions(msg.Width, msg.Height)
		// Trigger a screen clear and redraw after resize to prevent stale content
		return m, tea.Batch(
			tea.ClearScreen,
			func() tea.Msg { return clearScreenMsg{} },
		)

	case clearScreenMsg:
		// After clearing, just return to trigger a redraw
		return m, nil

	case tickMsg:
		// Refresh data periodically (with OAuth caching)
		return m, tea.Batch(
			loadDataCmdWithModel(m.config, &m),
			tickCmd(m.config.RefreshRate),
		)

	case dataLoadedMsg:
		if msg.err != nil {
			m.SetError(msg.err)
			return m, nil
		}

		// Handle OAuth errors - disable OAuth if permanent failure
		if msg.oauthErr != nil && msg.oauthDisabled {
			if !m.HasLoggedOAuthError() {
				log.Printf("OAuth disabled: %v (falling back to JSONL)", msg.oauthErr)
				m.MarkOAuthErrorLogged()
			}
			m.DisableOAuth(msg.oauthErr.Error())
		}

		// Store OAuth data if available
		if msg.oauthData != nil {
			m.SetOAuthData(msg.oauthData)
			m.lastOAuthFetch = time.Now()
		}

		// Process entries into sessions
		now := time.Now()
		sessions := analysis.CreateSessionBlocks(msg.entries)
		sessions = analysis.MarkActiveSessions(sessions, now)
		sessions = analysis.UpdateSessionCosts(sessions)

		// Calculate weekly usage using pre-computed session blocks
		weekly := analysis.CalculateWeeklyUsage(sessions, m.config.Plan, now)

		// Update model
		m.SetData(msg.entries, sessions, weekly)

		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

// View renders the application
func (m AppModel) View() string {
	if m.HasError() {
		return ui.DangerStyle.Render(fmt.Sprintf("Error: %v\n\nPress q to quit", m.GetError()))
	}

	if m.IsLoading() {
		return fmt.Sprintf("\n  %s Loading Claude usage data...\n\n", m.spinner.View())
	}

	if !m.HasData() {
		return ui.WarningStyle.Render("No usage data found.\n\nPress q to quit")
	}

	// Render based on view mode
	switch m.config.ViewMode {
	case models.ViewModeDaily:
		return ui.RenderDailyView(m.GetEntries(), m.width)
	case models.ViewModeMonthly:
		return ui.RenderMonthlyView(m.GetEntries(), m.width)
	default:
		// Create dashboard data
		data := ui.DashboardData{
			Config:         m.config,
			Limits:         m.limits,
			CurrentSession: m.currentSession,
			AllSessions:    m.sessions,
			WeeklyUsage:    m.weeklyUsage,
			OAuthData:      m.oauthData,
		}
		return ui.RenderDashboard(data)
	}
}

// loadDataCmd loads usage data in the background
func loadDataCmd(config *models.Config) tea.Cmd {
	return loadDataCmdWithModel(config, nil)
}

// loadDataCmdWithModel loads data with optional model context for OAuth caching
func loadDataCmdWithModel(config *models.Config, model *AppModel) tea.Cmd {
	return func() tea.Msg {
		var oauthData *oauth.UsageData
		var oauthErr error
		var oauthShouldDisable bool

		// Only fetch OAuth data if:
		// 1. OAuth is available
		// 2. OAuth hasn't been permanently disabled
		// 3. We haven't fetched in the last 60 seconds (or model is nil on first load)
		// 4. OR the cached OAuth data has a stale session (reset time already passed)
		oauthNotDisabled := model == nil || !model.IsOAuthDisabled()
		shouldFetchOAuth := oauth.IsAvailable() && oauthNotDisabled &&
			(model == nil || time.Since(model.lastOAuthFetch) > 60*time.Second || isOAuthSessionStale(model))

		if shouldFetchOAuth {
			client, err := oauth.NewClient()
			if err == nil {
				oauthData, err = client.FetchUsage()
				if err != nil {
					oauthErr = err
					oauthData = nil

					// Determine if this is a permanent failure
					if errors.Is(err, oauth.ErrTokenExpired) {
						oauthShouldDisable = true
					} else if !oauth.IsTransientError(err) {
						// Other non-transient errors also disable OAuth
						oauthShouldDisable = true
					}
					// For transient errors, we'll retry on the next tick
				}
			} else {
				oauthErr = err
				// Client creation failure is usually permanent (keychain issue)
				oauthShouldDisable = true
			}
		} else if model != nil && model.oauthData != nil {
			// Reuse cached OAuth data
			oauthData = model.oauthData
		}

		// Load at least 7 days (168 hours) of data for weekly usage calculations
		hoursToLoad := config.HoursBack
		if config.ShowWeekly && hoursToLoad < 168 {
			hoursToLoad = 168
		}
		entries, err := data.LoadUsageData(config.DataPath, hoursToLoad)

		return dataLoadedMsg{
			entries:       entries,
			oauthData:     oauthData,
			err:           err,
			oauthErr:      oauthErr,
			oauthDisabled: oauthShouldDisable,
		}
	}
}

// tickCmd returns a command that ticks at the given interval
func tickCmd(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// isOAuthSessionStale checks if the cached OAuth data has a stale session
// (i.e., the reset time + 5 hours has already passed)
func isOAuthSessionStale(model *AppModel) bool {
	if model == nil || model.oauthData == nil {
		return false
	}

	resetTime, err := oauth.ParseResetTime(model.oauthData.FiveHour.ResetsAt)
	if err != nil {
		return true // Force refresh if we can't parse
	}

	// The API returns the session start time as ResetsAt
	// The actual reset is start time + 5 hours
	// If even that is in the past, the data is stale
	actualResetTime := resetTime.Add(5 * time.Hour)
	return !actualResetTime.After(time.Now())
}
