package app

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/sammcj/ccu/internal/models"
	"github.com/sammcj/ccu/internal/oauth"
)

// AppModel is the Bubbletea application model
type AppModel struct {
	// Configuration
	config *models.Config

	// Data
	entries        []models.UsageEntry
	sessions       []models.SessionBlock
	weeklyUsage    models.WeeklyUsage
	currentSession *models.SessionBlock
	limits         models.Limits
	oauthData      *oauth.UsageData // OAuth-fetched usage data

	// State
	loading            bool
	err                error
	lastRefresh        time.Time
	lastOAuthFetch     time.Time // Track when OAuth was last fetched
	lastWeeklyFetch    time.Time // Track when weekly data was last refreshed from OAuth
	lastTickTime       time.Time // Track when last tick fired (for sleep detection)
	zeroRemainingStart time.Time // Track when remaining time first hit 0
	oauthEnabled       bool      // Whether OAuth is available
	oauthDisabled      bool      // Whether OAuth has been disabled due to permanent error
	oauthDisableReason string    // Reason OAuth was disabled (for UI display)
	oauthErrorLogged   bool      // Whether we've already logged the OAuth error
	forceRefresh       bool      // Force next refresh to bypass cache (after wake/focus)
	tickGeneration     uint64    // Incremented on resume to kill stale tick chains
	width              int
	height             int

	// Manual refresh rate limiting
	lastManualRefresh      time.Time // When last manual refresh was triggered
	manualRefreshCount     int       // Count of rapid manual refreshes (for backoff)
	rateLimitWarning       string    // Warning message to display
	rateLimitWarningExpiry time.Time // When to clear the warning

	// UI Components
	spinner spinner.Model
}

// NewModel creates a new application model
func NewModel(config *models.Config) *AppModel {
	s := spinner.New()
	s.Spinner = spinner.Dot

	oauthAvailable := oauth.IsAvailable()

	// Adjust refresh rate based on data source if using default value
	// OAuth: 60s (less frequent, more expensive API calls)
	// JSONL: 30s (default, local file parsing)
	if config.RefreshRate == 30*time.Second && oauthAvailable {
		config.RefreshRate = 60 * time.Second
	}

	return &AppModel{
		config:       config,
		loading:      true,
		spinner:      s,
		limits:       config.GetEffectiveLimits(),
		oauthEnabled: oauthAvailable,
	}
}

// GetConfig returns the application configuration
func (m *AppModel) GetConfig() *models.Config {
	return m.config
}

// SetDimensions updates the terminal dimensions
func (m *AppModel) SetDimensions(width, height int) {
	m.width = width
	m.height = height
}

// SetLoading sets the loading state
func (m *AppModel) SetLoading(loading bool) {
	m.loading = loading
}

// SetError sets an error state
func (m *AppModel) SetError(err error) {
	m.err = err
	m.loading = false
}

// SetData updates the model with new data
func (m *AppModel) SetData(entries []models.UsageEntry, sessions []models.SessionBlock, weekly models.WeeklyUsage) {
	m.entries = entries
	m.sessions = sessions
	m.weeklyUsage = weekly
	m.lastRefresh = time.Now()
	m.loading = false

	// Find current active session
	for i := range sessions {
		if sessions[i].IsActive && !sessions[i].IsGap {
			m.currentSession = &sessions[i]
			break
		}
	}

	// If no active session, use most recent
	if m.currentSession == nil {
		for i := len(sessions) - 1; i >= 0; i-- {
			if !sessions[i].IsGap {
				m.currentSession = &sessions[i]
				break
			}
		}
	}
}

// HasData returns true if the model has loaded data
func (m *AppModel) HasData() bool {
	return len(m.entries) > 0
}

// HasError returns true if there's an error
func (m *AppModel) HasError() bool {
	return m.err != nil
}

// GetError returns the current error
func (m *AppModel) GetError() error {
	return m.err
}

// IsLoading returns the loading state
func (m *AppModel) IsLoading() bool {
	return m.loading
}

// GetSessions returns the session blocks
func (m *AppModel) GetSessions() []models.SessionBlock {
	return m.sessions
}

// GetCurrentSession returns the current/most recent session
func (m *AppModel) GetCurrentSession() *models.SessionBlock {
	return m.currentSession
}

// GetWeeklyUsage returns the weekly usage data
func (m *AppModel) GetWeeklyUsage() models.WeeklyUsage {
	return m.weeklyUsage
}

// GetLimits returns the current plan limits
func (m *AppModel) GetLimits() models.Limits {
	return m.limits
}

// GetEntries returns all usage entries
func (m *AppModel) GetEntries() []models.UsageEntry {
	return m.entries
}

// SetOAuthData updates the model with OAuth usage data
func (m *AppModel) SetOAuthData(data *oauth.UsageData) {
	m.oauthData = data
}

// GetOAuthData returns the OAuth usage data
func (m *AppModel) GetOAuthData() *oauth.UsageData {
	return m.oauthData
}

// IsOAuthEnabled returns whether OAuth fetching is available
func (m *AppModel) IsOAuthEnabled() bool {
	return m.oauthEnabled
}

// HasOAuthData returns true if OAuth data has been fetched
func (m *AppModel) HasOAuthData() bool {
	return m.oauthData != nil
}

// DisableOAuth disables OAuth fetching due to a permanent error
func (m *AppModel) DisableOAuth(reason string) {
	m.oauthDisabled = true
	m.oauthDisableReason = reason
	m.oauthData = nil // Clear stale cached data so JSONL fallback is used
}

// IsOAuthDisabled returns true if OAuth has been disabled due to an error
func (m *AppModel) IsOAuthDisabled() bool {
	return m.oauthDisabled
}

// GetOAuthDisableReason returns the reason OAuth was disabled
func (m *AppModel) GetOAuthDisableReason() string {
	return m.oauthDisableReason
}

// MarkOAuthErrorLogged marks that we've logged the OAuth error
func (m *AppModel) MarkOAuthErrorLogged() {
	m.oauthErrorLogged = true
}

// HasLoggedOAuthError returns true if we've already logged the OAuth error
func (m *AppModel) HasLoggedOAuthError() bool {
	return m.oauthErrorLogged
}

// SetZeroRemainingStart marks when remaining time first hit 0
func (m *AppModel) SetZeroRemainingStart(t time.Time) {
	m.zeroRemainingStart = t
}

// GetZeroRemainingStart returns when remaining time first hit 0
func (m *AppModel) GetZeroRemainingStart() time.Time {
	return m.zeroRemainingStart
}

// ClearZeroRemainingStart clears the zero remaining tracking
func (m *AppModel) ClearZeroRemainingStart() {
	m.zeroRemainingStart = time.Time{}
}

// SetLastTickTime records when the last tick fired
func (m *AppModel) SetLastTickTime(t time.Time) {
	m.lastTickTime = t
}

// GetLastTickTime returns when the last tick fired
func (m *AppModel) GetLastTickTime() time.Time {
	return m.lastTickTime
}

// SetLastWeeklyFetch records when weekly data was last fetched
func (m *AppModel) SetLastWeeklyFetch(t time.Time) {
	m.lastWeeklyFetch = t
}

// GetLastWeeklyFetch returns when weekly data was last fetched
func (m *AppModel) GetLastWeeklyFetch() time.Time {
	return m.lastWeeklyFetch
}

// SetForceRefresh sets whether the next refresh should bypass cache
func (m *AppModel) SetForceRefresh(force bool) {
	m.forceRefresh = force
}

// ShouldForceRefresh returns true if next refresh should bypass cache
func (m *AppModel) ShouldForceRefresh() bool {
	return m.forceRefresh
}

// CheckManualRefreshRateLimit checks if a manual refresh is allowed
// Returns (allowed, waitDuration) - if not allowed, waitDuration indicates how long to wait
func (m *AppModel) CheckManualRefreshRateLimit() (bool, time.Duration) {
	now := time.Now()

	// Reset backoff if no manual refresh for 30 seconds
	if !m.lastManualRefresh.IsZero() && now.Sub(m.lastManualRefresh) >= 30*time.Second {
		m.manualRefreshCount = 0
	}

	// Calculate required interval based on backoff level
	// Level increases every 2 requests: 1s, 1s, 2s, 2s, 4s, 4s, 8s, 8s...
	level := m.manualRefreshCount / 2
	requiredInterval := time.Second * time.Duration(1<<level) // 2^level seconds
	if requiredInterval > 60*time.Second {
		requiredInterval = 60 * time.Second // Cap at 60s
	}

	// Check if enough time has passed
	if !m.lastManualRefresh.IsZero() {
		elapsed := now.Sub(m.lastManualRefresh)
		if elapsed < requiredInterval {
			return false, requiredInterval - elapsed
		}
	}

	return true, 0
}

// RecordManualRefresh records a successful manual refresh
func (m *AppModel) RecordManualRefresh() {
	now := time.Now()

	// Reset backoff if it's been 30s since last refresh
	if !m.lastManualRefresh.IsZero() && now.Sub(m.lastManualRefresh) >= 30*time.Second {
		m.manualRefreshCount = 0
	}

	m.lastManualRefresh = now
	m.manualRefreshCount++
}

// SetRateLimitWarning sets a temporary rate limit warning
func (m *AppModel) SetRateLimitWarning(msg string, duration time.Duration) {
	m.rateLimitWarning = msg
	m.rateLimitWarningExpiry = time.Now().Add(duration)
}

// GetRateLimitWarning returns the current rate limit warning if not expired
func (m *AppModel) GetRateLimitWarning() string {
	if m.rateLimitWarning != "" && time.Now().Before(m.rateLimitWarningExpiry) {
		return m.rateLimitWarning
	}
	return ""
}

// ClearExpiredRateLimitWarning clears the warning if expired
func (m *AppModel) ClearExpiredRateLimitWarning() {
	if m.rateLimitWarning != "" && time.Now().After(m.rateLimitWarningExpiry) {
		m.rateLimitWarning = ""
	}
}
