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
	entries        []models.UsageEntry
	oauthData      *oauth.UsageData
	err            error
	oauthErr       error // Separate OAuth error for proper handling
	oauthDisabled  bool  // Whether OAuth should be permanently disabled
	oauthFreshData bool  // Whether OAuth data was freshly fetched (not from cache)
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
		case " ", "r":
			// Manual refresh with rate limiting
			allowed, waitDuration := m.CheckManualRefreshRateLimit()
			if !allowed {
				m.SetRateLimitWarning(
					fmt.Sprintf("Rate limited manual refresh - %.0fs", waitDuration.Seconds()),
					2*time.Second,
				)
				return m, nil
			}
			m.RecordManualRefresh()
			m.SetForceRefresh(true)
			return m, loadDataCmdWithModel(m.config, &m)
		}

	case tea.FocusMsg:
		// Terminal regained focus - likely woke from sleep or switched back
		// Force an immediate refresh bypassing OAuth cache
		m.SetForceRefresh(true)
		return m, loadDataCmdWithModel(m.config, &m)

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
		now := time.Time(msg)
		// Detect wall clock jump (system wake from sleep)
		// If more time has passed than expected (2x refresh rate), force a fresh fetch
		if !m.lastTickTime.IsZero() {
			elapsed := now.Sub(m.lastTickTime)
			expectedMax := m.config.RefreshRate * 2
			if elapsed > expectedMax {
				// Significant time jump detected - likely woke from sleep
				m.SetForceRefresh(true)
			}
		}
		m.SetLastTickTime(now)

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
			// Only update timestamps when we fetched fresh data (not from cache)
			// This ensures the 15-minute weekly refresh check works correctly
			if msg.oauthFreshData {
				now := time.Now()
				m.lastOAuthFetch = now
				m.SetLastWeeklyFetch(now)
			}
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

	var content string

	// Render based on view mode
	switch m.config.ViewMode {
	case models.ViewModeDaily:
		content = ui.RenderDailyView(m.GetEntries(), m.width)
	case models.ViewModeMonthly:
		content = ui.RenderMonthlyView(m.GetEntries(), m.width)
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
		content = ui.RenderDashboard(data)
	}

	// Append rate limit warning if active
	if warning := m.GetRateLimitWarning(); warning != "" {
		content += "\n" + ui.WarningStyle.Render("  "+warning)
	}

	return content
}

// loadDataCmd loads usage data in the background
func loadDataCmd(config *models.Config) tea.Cmd {
	return loadDataCmdWithModel(config, nil)
}

// loadDataCmdWithModel loads data with optional model context for OAuth caching
func loadDataCmdWithModel(config *models.Config, model *AppModel) tea.Cmd {
	// Capture forceRefresh state before async execution
	forceRefresh := model != nil && model.ShouldForceRefresh()
	if forceRefresh && model != nil {
		model.SetForceRefresh(false) // Clear the flag immediately
	}

	return func() tea.Msg {
		var oauthData *oauth.UsageData
		var oauthErr error
		var oauthShouldDisable bool
		var oauthFreshData bool // Track if we fetched fresh data vs using cache

		// Only fetch OAuth data if:
		// 1. OAuth is available
		// 2. OAuth hasn't been permanently disabled
		// 3. We haven't fetched in the last 60 seconds (or model is nil on first load)
		// 4. OR the cached OAuth data has a stale session (reset time already passed)
		// 5. OR forceRefresh is set (wake from sleep, terminal focus regained)
		// 6. OR weekly data hasn't been refreshed in 15 minutes (safety net for guaranteed weekly updates)
		oauthNotDisabled := model == nil || !model.IsOAuthDisabled()
		// Weekly refresh safety net: ensure OAuth is fetched at least every 15 minutes for weekly data
		weeklyRefreshNeeded := model != nil && !model.GetLastWeeklyFetch().IsZero() && time.Since(model.GetLastWeeklyFetch()) >= 15*time.Minute
		shouldFetchOAuth := oauth.IsAvailable() && oauthNotDisabled &&
			(model == nil || forceRefresh || time.Since(model.lastOAuthFetch) >= 60*time.Second || isOAuthSessionStale(model) || weeklyRefreshNeeded)

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
				} else {
					// Successfully fetched fresh OAuth data
					oauthFreshData = true
				}
			} else {
				oauthErr = err
				// Client creation failure is usually permanent (keychain issue)
				oauthShouldDisable = true
			}
		} else if model != nil && model.oauthData != nil {
			// Reuse cached OAuth data (oauthFreshData remains false)
			oauthData = model.oauthData
		}

		// Load at least 7 days (168 hours) of data for weekly usage calculations
		hoursToLoad := config.HoursBack
		if config.ShowWeekly && hoursToLoad < 168 {
			hoursToLoad = 168
		}
		entries, err := data.LoadUsageData(config.DataPath, hoursToLoad)

		return dataLoadedMsg{
			entries:        entries,
			oauthData:      oauthData,
			err:            err,
			oauthErr:       oauthErr,
			oauthDisabled:  oauthShouldDisable,
			oauthFreshData: oauthFreshData,
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
// This triggers a refresh when:
// 1. The session window has completely ended (reset time + 5 hours is in the past)
// 2. The session just rolled over but utilisation is implausibly high (stale data)
// 3. The remaining time has been at 0 for more than 5 minutes (stuck after sleep)
// 4. More than 2 minutes since last data refresh (handles wake from sleep mid-session)
func isOAuthSessionStale(model *AppModel) bool {
	if model == nil || model.oauthData == nil {
		return false
	}

	now := time.Now()

	// Check if we've been asleep - if last refresh was > 2 minutes ago, force refresh
	// This handles the case where the computer wakes from sleep mid-session
	if !model.lastRefresh.IsZero() && now.Sub(model.lastRefresh) > 2*time.Minute {
		return true
	}

	resetTime, err := oauth.ParseResetTime(model.oauthData.FiveHour.ResetsAt)
	if err != nil {
		return true // Force refresh if we can't parse
	}

	// The API returns the session start time as ResetsAt
	// The actual reset is start time + 5 hours
	actualResetTime := resetTime.Add(5 * time.Hour)

	// If the session window has completely ended, data is stale
	if !actualResetTime.After(now) {
		return true
	}

	// Check if session just rolled over but utilisation is implausibly high
	// This indicates the API returned stale utilisation data
	sessionJustRolledOver := !resetTime.After(now)
	if sessionJustRolledOver {
		elapsed := now.Sub(resetTime)
		// Maximum reasonable utilisation = (elapsed / 5 hours) * 100
		maxReasonablePercent := (elapsed.Hours() / 5.0) * 100
		if maxReasonablePercent < 1 {
			maxReasonablePercent = 1
		}

		// If utilisation is much higher than possible, data is stale
		if model.oauthData.FiveHour.Utilisation > maxReasonablePercent*2 {
			return true
		}
	}

	// Check if remaining time has been at 0 for more than 5 minutes
	// This handles the case where the app wakes from sleep showing stale data
	remaining := time.Until(actualResetTime)
	if remaining <= 0 {
		// Remaining is at 0 - track when this started
		if model.zeroRemainingStart.IsZero() {
			model.SetZeroRemainingStart(now)
		} else if now.Sub(model.zeroRemainingStart) > 5*time.Minute {
			// Been at 0 for over 5 minutes - force refresh
			model.ClearZeroRemainingStart()
			return true
		}
	} else {
		// Remaining time is positive - clear the zero tracking
		model.ClearZeroRemainingStart()
	}

	return false
}
