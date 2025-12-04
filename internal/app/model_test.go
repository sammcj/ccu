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
		name       string
		resetAt    string // RFC3339 format
		wantStale  bool
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
			name:      "reset time 4 hours ago - not stale (within 5hr window)",
			resetAt:   time.Now().Add(-4 * time.Hour).Format(time.RFC3339Nano),
			wantStale: false,
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

			got := isOAuthSessionStale(model)
			assert.Equal(t, tt.wantStale, got, "isOAuthSessionStale() mismatch")
		})
	}
}
