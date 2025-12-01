package oauth

import (
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// skipIfNotMacOS skips the test if not running on macOS
func skipIfNotMacOS(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("Skipping macOS keychain test on non-macOS platform")
	}
}

func TestParseResetTime(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantError bool
	}{
		{
			name:      "valid RFC3339 with nanoseconds",
			input:     "2025-12-01T11:00:00.201690+00:00",
			wantError: false,
		},
		{
			name:      "valid RFC3339 without nanoseconds",
			input:     "2025-12-01T11:00:00+00:00",
			wantError: false,
		},
		{
			name:      "invalid format",
			input:     "2025-12-01 11:00:00",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseResetTime(tt.input)

			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.False(t, result.IsZero())
			}
		})
	}
}

func TestIsAvailable(t *testing.T) {
	skipIfNotMacOS(t)

	available := IsAvailable()
	// We can't assert true/false because it depends on the environment
	// Just verify it doesn't panic
	t.Logf("OAuth available: %v", available)
}

func TestNewClient(t *testing.T) {
	skipIfNotMacOS(t)

	client, err := NewClient()

	// If error, check if it's the expected "no credentials" error
	if err != nil {
		t.Logf("Expected error in test environment: %v", err)
		return
	}

	// If we got a client, verify it's properly initialised
	assert.NotNil(t, client)
	assert.NotNil(t, client.httpClient)
	assert.NotNil(t, client.token)
}

func TestFetchUsage(t *testing.T) {
	skipIfNotMacOS(t)

	client, err := NewClient()
	if err != nil {
		t.Skipf("Cannot test FetchUsage without OAuth credentials: %v", err)
	}

	usage, err := client.FetchUsage()
	if err != nil {
		// Network errors are environmental and shouldn't fail the test
		t.Skipf("FetchUsage skipped due to network/environment issue: %v", err)
	}

	// Verify we got valid data
	assert.NotNil(t, usage)
	assert.GreaterOrEqual(t, usage.FiveHour.Utilisation, 0.0)
	assert.LessOrEqual(t, usage.FiveHour.Utilisation, 100.0)
	assert.NotEmpty(t, usage.FiveHour.ResetsAt)

	// Verify reset time is parseable
	resetTime, err := ParseResetTime(usage.FiveHour.ResetsAt)
	assert.NoError(t, err)
	assert.True(t, resetTime.After(time.Now().Add(-24*time.Hour)))

	t.Logf("5-hour utilisation: %.1f%%", usage.FiveHour.Utilisation)
	t.Logf("7-day utilisation: %.1f%%", usage.SevenDay.Utilisation)

	if usage.SevenDaySonnet != nil {
		t.Logf("7-day Sonnet: %.1f%%", usage.SevenDaySonnet.Utilisation)
	}

	if usage.SevenDayOpus != nil {
		t.Logf("7-day Opus: %.1f%%", usage.SevenDayOpus.Utilisation)
	}
}
