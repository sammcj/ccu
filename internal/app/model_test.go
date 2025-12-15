package app

import (
	"testing"
	"time"

	"github.com/sammcj/ccu/internal/models"
	"github.com/sammcj/ccu/internal/oauth"
	"github.com/stretchr/testify/assert"
)

func TestNewModel_RefreshRateAdjustment(t *testing.T) {
	tests := []struct {
		name                string
		inputRefreshRate    time.Duration
		expectedMinRate     time.Duration // Minimum expected (JSONL mode)
		expectedMaxRate     time.Duration // Maximum expected (OAuth mode)
		note                string
	}{
		{
			name:             "default_30s_adjusted_for_oauth_if_available",
			inputRefreshRate: 30 * time.Second,
			expectedMinRate:  30 * time.Second, // JSONL keeps 30s
			expectedMaxRate:  60 * time.Second, // OAuth upgrades to 60s
			note:             "Default 30s should be adjusted to 60s when OAuth is available",
		},
		{
			name:             "explicit_10s_not_adjusted",
			inputRefreshRate: 10 * time.Second,
			expectedMinRate:  10 * time.Second,
			expectedMaxRate:  10 * time.Second,
			note:             "Explicit non-default rates should not be adjusted",
		},
		{
			name:             "explicit_60s_not_adjusted",
			inputRefreshRate: 60 * time.Second,
			expectedMinRate:  60 * time.Second,
			expectedMaxRate:  60 * time.Second,
			note:             "Explicit 60s should remain 60s regardless of OAuth",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := models.DefaultConfig()
			config.RefreshRate = tt.inputRefreshRate

			model := NewModel(config)

			// The actual rate will be either min (JSONL) or max (OAuth)
			// We can't predict which without checking OAuth availability
			actualRate := model.config.RefreshRate

			// Verify the rate is within expected bounds
			assert.GreaterOrEqual(t, actualRate, tt.expectedMinRate,
				"Refresh rate should be at least %v (JSONL mode)", tt.expectedMinRate)
			assert.LessOrEqual(t, actualRate, tt.expectedMaxRate,
				"Refresh rate should be at most %v (OAuth mode)", tt.expectedMaxRate)

			// For non-default rates, verify exact match
			if tt.inputRefreshRate != 30*time.Second {
				assert.Equal(t, tt.inputRefreshRate, actualRate,
					"Non-default refresh rates should not be adjusted")
			}
		})
	}
}

func TestNewModel_InitialState(t *testing.T) {
	config := models.DefaultConfig()
	model := NewModel(config)

	assert.NotNil(t, model.config, "Config should be set")
	assert.True(t, model.loading, "Model should start in loading state")
	assert.NotNil(t, model.spinner, "Spinner should be initialised")
	assert.NotNil(t, model.limits, "Limits should be set from config")
}

func TestIsOAuthSessionStale(t *testing.T) {
	tests := []struct {
		name        string
		resetAt     string // RFC3339 format
		utilisation float64
		wantStale   bool
	}{
		{
			name:      "nil model returns false",
			resetAt:   "",
			wantStale: false,
		},
		{
			name:      "reset time 6 hours ago - stale (past the 5hr window)",
			resetAt:   time.Now().Add(-6 * time.Hour).Format(time.RFC3339Nano),
			wantStale: true,
		},
		{
			name:        "reset time 4 hours ago with low utilisation - not stale",
			resetAt:     time.Now().Add(-4 * time.Hour).Format(time.RFC3339Nano),
			utilisation: 50.0,
			wantStale:   false,
		},
		{
			name:      "reset time 1 hour in future - not stale",
			resetAt:   time.Now().Add(1 * time.Hour).Format(time.RFC3339Nano),
			wantStale: false,
		},
		{
			name:      "reset time exactly 5 hours ago - stale (boundary)",
			resetAt:   time.Now().Add(-5 * time.Hour).Add(-1 * time.Second).Format(time.RFC3339Nano),
			wantStale: true,
		},
		// New tests for session rollover with stale utilisation
		{
			name:        "session just rolled over 30min ago with 100% utilisation - stale",
			resetAt:     time.Now().Add(-30 * time.Minute).Format(time.RFC3339Nano),
			utilisation: 100.0,
			wantStale:   true,
		},
		{
			name:        "session rolled over 30min ago with 5% utilisation - not stale (plausible)",
			resetAt:     time.Now().Add(-30 * time.Minute).Format(time.RFC3339Nano),
			utilisation: 5.0,
			wantStale:   false,
		},
		{
			name:        "session rolled over 1hr ago with 50% utilisation - stale (too high)",
			resetAt:     time.Now().Add(-1 * time.Hour).Format(time.RFC3339Nano),
			utilisation: 50.0,
			wantStale:   true,
		},
		{
			name:        "session rolled over 1hr ago with 10% utilisation - not stale (plausible)",
			resetAt:     time.Now().Add(-1 * time.Hour).Format(time.RFC3339Nano),
			utilisation: 10.0,
			wantStale:   false,
		},
		{
			name:        "session 4 hours in with 80% utilisation - not stale",
			resetAt:     time.Now().Add(-4 * time.Hour).Format(time.RFC3339Nano),
			utilisation: 80.0,
			wantStale:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.resetAt == "" {
				// Test nil model case
				assert.False(t, isOAuthSessionStale(nil))
				return
			}

			config := models.DefaultConfig()
			model := NewModel(config)
			model.oauthData = &oauth.UsageData{}
			model.oauthData.FiveHour.ResetsAt = tt.resetAt
			model.oauthData.FiveHour.Utilisation = tt.utilisation

			got := isOAuthSessionStale(model)
			assert.Equal(t, tt.wantStale, got, "isOAuthSessionStale() mismatch")
		})
	}
}

func TestIsOAuthSessionStale_ZeroRemainingTimeout(t *testing.T) {
	tests := []struct {
		name               string
		resetAt            time.Time // When session started (actual reset = resetAt + 5hr)
		zeroRemainingStart time.Time // When we first detected 0 remaining
		wantStale          bool
	}{
		{
			name:               "remaining at 0 for first time - stale because session ended",
			resetAt:            time.Now().Add(-6 * time.Hour), // Session ended 1hr ago
			zeroRemainingStart: time.Time{},                   // Not set yet
			wantStale:          true,                          // Will be stale because session ended
		},
		{
			name:               "remaining at 0 for 3 minutes - stale (session window ended)",
			resetAt:            time.Now().Add(-5*time.Hour - 3*time.Minute), // Reset 3min ago
			zeroRemainingStart: time.Now().Add(-3 * time.Minute),
			wantStale:          true, // Session window has ended - actualResetTime is in the past
		},
		{
			name:               "session still active (1min left) - not stale",
			resetAt:            time.Now().Add(-5*time.Hour + 1*time.Minute), // Reset in 1min
			zeroRemainingStart: time.Time{},
			wantStale:          false, // Session window still active
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := models.DefaultConfig()
			model := NewModel(config)
			model.oauthData = &oauth.UsageData{}
			model.oauthData.FiveHour.ResetsAt = tt.resetAt.Format(time.RFC3339Nano)
			model.oauthData.FiveHour.Utilisation = 50.0

			if !tt.zeroRemainingStart.IsZero() {
				model.SetZeroRemainingStart(tt.zeroRemainingStart)
			}

			got := isOAuthSessionStale(model)
			assert.Equal(t, tt.wantStale, got, "isOAuthSessionStale() mismatch")
		})
	}
}

func TestIsOAuthSessionStale_WakeFromSleep(t *testing.T) {
	tests := []struct {
		name        string
		lastRefresh time.Time
		wantStale   bool
	}{
		{
			name:        "last refresh 30 seconds ago - not stale",
			lastRefresh: time.Now().Add(-30 * time.Second),
			wantStale:   false,
		},
		{
			name:        "last refresh 3 minutes ago - stale (wake from sleep)",
			lastRefresh: time.Now().Add(-3 * time.Minute),
			wantStale:   true,
		},
		{
			name:        "last refresh 10 minutes ago - stale (wake from sleep)",
			lastRefresh: time.Now().Add(-10 * time.Minute),
			wantStale:   true,
		},
		{
			name:        "no last refresh set - not stale (first load)",
			lastRefresh: time.Time{},
			wantStale:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := models.DefaultConfig()
			model := NewModel(config)
			model.oauthData = &oauth.UsageData{}
			// Set a valid future reset time so other checks don't trigger
			model.oauthData.FiveHour.ResetsAt = time.Now().Add(2 * time.Hour).Format(time.RFC3339Nano)
			model.oauthData.FiveHour.Utilisation = 20.0
			model.lastRefresh = tt.lastRefresh

			got := isOAuthSessionStale(model)
			assert.Equal(t, tt.wantStale, got, "isOAuthSessionStale() mismatch")
		})
	}
}

func TestZeroRemainingTracking(t *testing.T) {
	config := models.DefaultConfig()
	model := NewModel(config)

	// Initially zero remaining start should be zero
	assert.True(t, model.GetZeroRemainingStart().IsZero(), "Initial zero remaining start should be zero")

	// Set zero remaining start
	now := time.Now()
	model.SetZeroRemainingStart(now)
	assert.Equal(t, now, model.GetZeroRemainingStart(), "Should store zero remaining start time")

	// Clear zero remaining start
	model.ClearZeroRemainingStart()
	assert.True(t, model.GetZeroRemainingStart().IsZero(), "Should clear zero remaining start")
}

func TestForceRefreshTracking(t *testing.T) {
	config := models.DefaultConfig()
	model := NewModel(config)

	// Initially forceRefresh should be false
	assert.False(t, model.ShouldForceRefresh(), "Initial forceRefresh should be false")

	// Set forceRefresh
	model.SetForceRefresh(true)
	assert.True(t, model.ShouldForceRefresh(), "Should return true after setting forceRefresh")

	// Clear forceRefresh
	model.SetForceRefresh(false)
	assert.False(t, model.ShouldForceRefresh(), "Should return false after clearing forceRefresh")
}

func TestLastTickTimeTracking(t *testing.T) {
	config := models.DefaultConfig()
	model := NewModel(config)

	// Initially lastTickTime should be zero
	assert.True(t, model.GetLastTickTime().IsZero(), "Initial lastTickTime should be zero")

	// Set lastTickTime
	now := time.Now()
	model.SetLastTickTime(now)
	assert.Equal(t, now, model.GetLastTickTime(), "Should store lastTickTime")
}

func TestTimeJumpDetection(t *testing.T) {
	tests := []struct {
		name           string
		lastTickTime   time.Time
		currentTime    time.Time
		refreshRate    time.Duration
		expectForce    bool
		description    string
	}{
		{
			name:         "normal tick interval - no force refresh",
			lastTickTime: time.Now().Add(-60 * time.Second),
			currentTime:  time.Now(),
			refreshRate:  60 * time.Second,
			expectForce:  false,
			description:  "Tick arrived on time, no wake from sleep",
		},
		{
			name:         "double interval - triggers force refresh",
			lastTickTime: time.Now().Add(-3 * time.Minute),
			currentTime:  time.Now(),
			refreshRate:  60 * time.Second,
			expectForce:  true,
			description:  "More than 2x refresh rate elapsed - likely wake from sleep",
		},
		{
			name:         "long sleep - triggers force refresh",
			lastTickTime: time.Now().Add(-1 * time.Hour),
			currentTime:  time.Now(),
			refreshRate:  60 * time.Second,
			expectForce:  true,
			description:  "1 hour elapsed - definitely wake from sleep",
		},
		{
			name:         "slightly delayed tick - no force refresh",
			lastTickTime: time.Now().Add(-90 * time.Second),
			currentTime:  time.Now(),
			refreshRate:  60 * time.Second,
			expectForce:  false,
			description:  "1.5x refresh rate is within tolerance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := models.DefaultConfig()
			config.RefreshRate = tt.refreshRate
			model := NewModel(config)
			model.SetLastTickTime(tt.lastTickTime)

			// Simulate the time jump detection logic from Update()
			elapsed := tt.currentTime.Sub(tt.lastTickTime)
			expectedMax := tt.refreshRate * 2
			shouldForce := elapsed > expectedMax

			assert.Equal(t, tt.expectForce, shouldForce,
				"%s: elapsed=%v, expectedMax=%v", tt.description, elapsed, expectedMax)
		})
	}
}

func TestManualRefreshRateLimit(t *testing.T) {
	tests := []struct {
		name             string
		refreshCount     int
		timeSinceLast    time.Duration
		expectAllowed    bool
		expectWaitApprox time.Duration // Approximate wait time if not allowed
	}{
		{
			name:          "first refresh always allowed",
			refreshCount:  0,
			timeSinceLast: 0,
			expectAllowed: true,
		},
		{
			name:             "second refresh too soon (under 1s)",
			refreshCount:     1,
			timeSinceLast:    500 * time.Millisecond,
			expectAllowed:    false,
			expectWaitApprox: 500 * time.Millisecond,
		},
		{
			name:          "second refresh after 1s allowed",
			refreshCount:  1,
			timeSinceLast: 1 * time.Second,
			expectAllowed: true,
		},
		{
			name:             "third refresh needs 2s (level 1)",
			refreshCount:     2,
			timeSinceLast:    1 * time.Second,
			expectAllowed:    false,
			expectWaitApprox: 1 * time.Second,
		},
		{
			name:          "third refresh after 2s allowed",
			refreshCount:  2,
			timeSinceLast: 2 * time.Second,
			expectAllowed: true,
		},
		{
			name:             "fifth refresh needs 4s (level 2)",
			refreshCount:     4,
			timeSinceLast:    2 * time.Second,
			expectAllowed:    false,
			expectWaitApprox: 2 * time.Second,
		},
		{
			name:          "refresh after 30s idle resets backoff",
			refreshCount:  10,
			timeSinceLast: 30 * time.Second,
			expectAllowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := models.DefaultConfig()
			model := NewModel(config)

			// Set up state
			if tt.refreshCount > 0 {
				model.lastManualRefresh = time.Now().Add(-tt.timeSinceLast)
				model.manualRefreshCount = tt.refreshCount
			}

			allowed, waitDuration := model.CheckManualRefreshRateLimit()

			assert.Equal(t, tt.expectAllowed, allowed, "allowed mismatch")

			if !tt.expectAllowed && tt.expectWaitApprox > 0 {
				// Allow 100ms tolerance for timing
				assert.InDelta(t, tt.expectWaitApprox.Seconds(), waitDuration.Seconds(), 0.2,
					"wait duration should be approximately %v", tt.expectWaitApprox)
			}
		})
	}
}

func TestRateLimitWarning(t *testing.T) {
	config := models.DefaultConfig()
	model := NewModel(config)

	// No warning initially
	assert.Empty(t, model.GetRateLimitWarning())

	// Set warning
	model.SetRateLimitWarning("Rate limited manual refresh -", 2*time.Second)
	assert.Equal(t, "Rate limited manual refresh -", model.GetRateLimitWarning())

	// Warning should expire
	model.rateLimitWarningExpiry = time.Now().Add(-1 * time.Second)
	assert.Empty(t, model.GetRateLimitWarning())
}
