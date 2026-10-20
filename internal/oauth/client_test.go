package oauth

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestIsTransientError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "token expired is not transient",
			err:      ErrTokenExpired,
			expected: false,
		},
		{
			name:     "network error is transient",
			err:      ErrNetworkError,
			expected: true,
		},
		{
			name:     "rate limit error is transient",
			err:      &RateLimitError{RetryAfter: 60 * time.Second},
			expected: true,
		},
		{
			name:     "rate limit error without retry-after is transient",
			err:      &RateLimitError{},
			expected: true,
		},
		{
			name:     "timeout error is transient",
			err:      assert.AnError, // Using generic error with timeout in message below
			expected: false,          // generic error without keyword is not transient
		},
		{
			name:     "connection error is transient",
			err:      &testError{msg: "dial tcp: connection refused"},
			expected: true,
		},
		{
			name:     "EOF error is transient",
			err:      &testError{msg: "unexpected EOF"},
			expected: true,
		},
		{
			name:     "timeout in message is transient",
			err:      &testError{msg: "context deadline exceeded (Client.Timeout exceeded)"},
			expected: true,
		},
		{
			name:     "unknown API error is not transient",
			err:      &testError{msg: "API returned status 500"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsTransientError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected time.Duration
	}{
		{
			name:     "empty header returns default",
			header:   "",
			expected: 2 * time.Minute,
		},
		{
			name:     "seconds value",
			header:   "60",
			expected: 60 * time.Second,
		},
		{
			name:     "small seconds value",
			header:   "5",
			expected: 5 * time.Second,
		},
		{
			name:     "zero returns default",
			header:   "0",
			expected: 2 * time.Minute,
		},
		{
			name:     "negative returns default",
			header:   "-10",
			expected: 2 * time.Minute,
		},
		{
			name:     "unparseable returns default",
			header:   "not-a-number",
			expected: 2 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseRetryAfter(tt.header)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRateLimitError(t *testing.T) {
	t.Run("with retry-after", func(t *testing.T) {
		err := &RateLimitError{RetryAfter: 90 * time.Second}
		assert.Contains(t, err.Error(), "retry after")
		assert.ErrorIs(t, err, ErrRateLimited)
	})

	t.Run("without retry-after", func(t *testing.T) {
		err := &RateLimitError{}
		assert.Equal(t, "rate limited by API", err.Error())
		assert.ErrorIs(t, err, ErrRateLimited)
	})
}

func TestGetRetryAfter(t *testing.T) {
	t.Run("rate limit error", func(t *testing.T) {
		err := &RateLimitError{RetryAfter: 45 * time.Second}
		assert.Equal(t, 45*time.Second, GetRetryAfter(err))
	})

	t.Run("non-rate-limit error", func(t *testing.T) {
		err := &testError{msg: "some other error"}
		assert.Equal(t, time.Duration(0), GetRetryAfter(err))
	})

	t.Run("nil error", func(t *testing.T) {
		assert.Equal(t, time.Duration(0), GetRetryAfter(nil))
	})
}

func TestIsAvailableCaching(t *testing.T) {
	origCheck := checkCredentials
	t.Cleanup(func() {
		availabilityMu.Lock()
		checkCredentials = origCheck
		availabilityAt = time.Time{}
		availabilityMu.Unlock()
	})

	resetCacheAge := func(age time.Duration) {
		availabilityMu.Lock()
		if age < 0 {
			availabilityAt = time.Time{}
		} else {
			availabilityAt = time.Now().Add(-age)
		}
		availabilityMu.Unlock()
	}

	calls := 0
	checkCredentials = func() bool {
		calls++
		return true
	}

	resetCacheAge(-1) // Empty cache forces a fresh check
	assert.True(t, IsAvailable())
	assert.True(t, IsAvailable(), "cached positive result should be returned")
	assert.Equal(t, 1, calls, "second call within TTL should not re-check the keychain")

	// Expire the cache and flip the underlying result
	checkCredentials = func() bool {
		calls++
		return false
	}
	resetCacheAge(availabilityTTL + time.Second)
	assert.False(t, IsAvailable(), "expired cache should trigger a fresh check")
	assert.False(t, IsAvailable(), "negative results should be cached too")
	assert.Equal(t, 2, calls, "only one fresh check after expiry")
}

func TestRequiresUserAction(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "token expired auto-retries (Claude Code refreshes the token)",
			err:      ErrTokenExpired,
			expected: false,
		},
		{
			name:     "missing scope requires re-authentication",
			err:      ErrMissingScope,
			expected: true,
		},
		{
			name:     "wrapped missing scope is still detected",
			err:      fmt.Errorf("failed to retrieve OAuth credentials: %w", ErrMissingScope),
			expected: true,
		},
		{
			name:     "setup-token scope error auto-retries so a fresh Keychain login is picked up",
			err:      ErrEnvTokenScope,
			expected: false,
		},
		{
			name:     "expired keychain token auto-retries",
			err:      &TokenExpiredError{Source: sourceKeychain, AltAuth: "ANTHROPIC_API_KEY"},
			expected: false,
		},
		{
			name:     "rate limit error does not require user action",
			err:      &RateLimitError{},
			expected: false,
		},
		{
			name:     "generic error does not require user action",
			err:      &testError{msg: "boom"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, RequiresUserAction(tt.err))
		})
	}
}

// testError is a simple error type for testing
type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

func TestEffectiveFiveHour(t *testing.T) {
	now := time.Date(2025, 12, 3, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name         string
		resetsAt     string
		utilisation  float64
		wantPercent  float64
		wantResetsAt time.Time
		wantStale    bool
	}{
		{
			name:         "not rolled over - raw utilisation and reset time",
			resetsAt:     now.Add(2 * time.Hour).Format(time.RFC3339),
			utilisation:  87.5,
			wantPercent:  87.5,
			wantResetsAt: now.Add(2 * time.Hour),
			wantStale:    false,
		},
		{
			name:         "rolled over 30min ago with implausibly high utilisation - stale, clamped to 0",
			resetsAt:     now.Add(-30 * time.Minute).Format(time.RFC3339),
			utilisation:  95,
			wantPercent:  0,
			wantResetsAt: now.Add(4*time.Hour + 30*time.Minute),
			wantStale:    true,
		},
		{
			name:         "rolled over 1hr ago with plausible utilisation - kept, reset advanced 5h",
			resetsAt:     now.Add(-1 * time.Hour).Format(time.RFC3339),
			utilisation:  15,
			wantPercent:  15,
			wantResetsAt: now.Add(4 * time.Hour),
			wantStale:    false,
		},
		{
			name:         "rolled over 4hr ago at the plausibility boundary - not stale (max ~80%, doubled)",
			resetsAt:     now.Add(-4 * time.Hour).Format(time.RFC3339),
			utilisation:  80,
			wantPercent:  80,
			wantResetsAt: now.Add(1 * time.Hour),
			wantStale:    false,
		},
		{
			name:        "unparseable resets_at - raw utilisation, not stale",
			resetsAt:    "not-a-timestamp",
			utilisation: 42,
			wantPercent: 42,
			wantStale:   false,
		},
		{
			name:         "just rolled over - 1% elapsed floor allows up to 2%",
			resetsAt:     now.Add(-time.Second).Format(time.RFC3339),
			utilisation:  1.5,
			wantPercent:  1.5,
			wantResetsAt: now.Add(5*time.Hour - time.Second),
			wantStale:    false,
		},
		{
			name:         "just rolled over - above doubled 1% floor is stale",
			resetsAt:     now.Add(-time.Second).Format(time.RFC3339),
			utilisation:  2.5,
			wantPercent:  0,
			wantResetsAt: now.Add(5*time.Hour - time.Second),
			wantStale:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &UsageData{}
			data.FiveHour.ResetsAt = tt.resetsAt
			data.FiveHour.Utilisation = tt.utilisation

			percent, resetsAt, stale := data.EffectiveFiveHour(now)

			assert.Equal(t, tt.wantPercent, percent)
			assert.Equal(t, tt.wantStale, stale)
			if !tt.wantResetsAt.IsZero() {
				assert.True(t, resetsAt.Equal(tt.wantResetsAt),
					"resetsAt = %v, want %v", resetsAt, tt.wantResetsAt)
			}
		})
	}
}

func fakeEnv(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func TestResolveCredentials(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	fresh := &ClaudeAiOAuth{AccessToken: "kc-fresh", ExpiresAt: now.Add(time.Hour).UnixMilli()}
	stale := &ClaudeAiOAuth{AccessToken: "kc-stale", ExpiresAt: now.Add(-time.Hour).UnixMilli()}
	errNoEntry := errors.New("keychain access failed")
	keychain := func(c *ClaudeAiOAuth, err error) func() (*ClaudeAiOAuth, error) {
		return func() (*ClaudeAiOAuth, error) { return c, err }
	}

	tests := []struct {
		name        string
		keychain    func() (*ClaudeAiOAuth, error)
		env         map[string]string
		wantToken   string
		wantSource  string
		wantAltAuth string
		wantErr     error
	}{
		{
			name:       "fresh keychain token with no env",
			keychain:   keychain(fresh, nil),
			wantToken:  "kc-fresh",
			wantSource: sourceKeychain,
		},
		{
			name:     "API-key-only user without a keychain entry gets the keychain error",
			keychain: keychain(nil, errNoEntry),
			env:      map[string]string{"ANTHROPIC_API_KEY": "sk-test"},
			wantErr:  errNoEntry,
		},
		{
			name:        "fresh keychain token wins over env token",
			keychain:    keychain(fresh, nil),
			env:         map[string]string{envOAuthToken: "env-tok", "ANTHROPIC_API_KEY": "sk-test"},
			wantToken:   "kc-fresh",
			wantSource:  sourceKeychain,
			wantAltAuth: "ANTHROPIC_API_KEY",
		},
		{
			name:       "expired keychain token falls back to env token",
			keychain:   keychain(stale, nil),
			env:        map[string]string{envOAuthToken: "env-tok"},
			wantToken:  "env-tok",
			wantSource: envOAuthToken,
		},
		{
			name:       "missing keychain entry falls back to env token",
			keychain:   keychain(nil, errors.New("keychain access failed")),
			env:        map[string]string{envOAuthToken: " env-tok\n"},
			wantToken:  "env-tok",
			wantSource: envOAuthToken,
		},
		{
			name:     "keychain error surfaces when no env token is set",
			keychain: keychain(nil, ErrMissingScope),
			env:      map[string]string{envOAuthToken: "   "},
			wantErr:  ErrMissingScope,
		},
		{
			name:        "expired keychain token is kept when nothing else exists",
			keychain:    keychain(stale, nil),
			env:         map[string]string{"ANTHROPIC_API_KEY": "sk-test"},
			wantToken:   "kc-stale",
			wantSource:  sourceKeychain,
			wantAltAuth: "ANTHROPIC_API_KEY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveCredentials(tt.keychain, fakeEnv(tt.env), now)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantToken, got.token.AccessToken)
			assert.Equal(t, tt.wantSource, got.source)
			assert.Equal(t, tt.wantAltAuth, got.altAuth)
		})
	}
}

func TestAltAuthEnv(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "nothing set", env: nil, want: ""},
		{name: "api key", env: map[string]string{"ANTHROPIC_API_KEY": "sk-test"}, want: "ANTHROPIC_API_KEY"},
		{name: "disabled flag ignored", env: map[string]string{"CLAUDE_CODE_USE_BEDROCK": "0"}, want: ""},
		{name: "false flag ignored", env: map[string]string{"CLAUDE_CODE_USE_VERTEX": "False"}, want: ""},
		{
			name: "follows Claude Code precedence",
			env:  map[string]string{"ANTHROPIC_API_KEY": "sk-test", "CLAUDE_CODE_USE_BEDROCK": "1"},
			want: "CLAUDE_CODE_USE_BEDROCK",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, altAuthEnv(fakeEnv(tt.env)))
		})
	}
}

func TestTokenExpiredErrorMessage(t *testing.T) {
	tests := []struct {
		name     string
		err      *TokenExpiredError
		contains string
	}{
		{
			name:     "env token points at a Keychain login",
			err:      &TokenExpiredError{Source: envOAuthToken},
			contains: "claude logout && claude login",
		},
		{
			name:     "alt auth explains why the keychain token is stale",
			err:      &TokenExpiredError{Source: sourceKeychain, AltAuth: "ANTHROPIC_API_KEY"},
			contains: "ANTHROPIC_API_KEY is set so Claude Code isn't refreshing it",
		},
		{
			name:     "known expiry is shown",
			err:      &TokenExpiredError{Source: sourceKeychain, ExpiresAt: time.Date(2026, 10, 20, 9, 0, 0, 0, time.Local)},
			contains: "expired 20 Oct 09:00",
		},
		{
			name:     "unknown expiry falls back to the generic message",
			err:      &TokenExpiredError{Source: sourceKeychain},
			contains: ErrTokenExpired.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Contains(t, tt.err.Error(), tt.contains)
			assert.ErrorIs(t, tt.err, ErrTokenExpired, "must stay on the auto-retry path")
		})
	}
}

func TestFetchUsageAuthHandling(t *testing.T) {
	future := time.Now().Add(time.Hour).UnixMilli()
	past := time.Now().Add(-time.Hour).UnixMilli()

	tests := []struct {
		name         string
		creds        credentials
		status       int
		wantRequests int
		wantErr      error
		wantMsg      string
	}{
		{
			name:         "expired keychain token skips the request",
			creds:        credentials{token: &ClaudeAiOAuth{AccessToken: "kc", ExpiresAt: past}, source: sourceKeychain, altAuth: "ANTHROPIC_API_KEY"},
			wantRequests: 0,
			wantErr:      ErrTokenExpired,
			wantMsg:      "ANTHROPIC_API_KEY is set",
		},
		{
			name:         "keychain 401 is token expiry",
			creds:        credentials{token: &ClaudeAiOAuth{AccessToken: "kc", ExpiresAt: future}, source: sourceKeychain},
			status:       http.StatusUnauthorized,
			wantRequests: 1,
			wantErr:      ErrTokenExpired,
		},
		{
			name:         "revoked keychain token doesn't blame the API key",
			creds:        credentials{token: &ClaudeAiOAuth{AccessToken: "kc", ExpiresAt: future}, source: sourceKeychain, altAuth: "ANTHROPIC_API_KEY"},
			status:       http.StatusUnauthorized,
			wantRequests: 1,
			wantErr:      ErrTokenExpired,
			wantMsg:      ErrTokenExpired.Error(),
		},
		{
			name:         "env token 403 is the setup-token scope gap",
			creds:        credentials{token: &ClaudeAiOAuth{AccessToken: "env"}, source: envOAuthToken},
			status:       http.StatusForbidden,
			wantRequests: 1,
			wantErr:      ErrEnvTokenScope,
		},
		{
			name:         "env token 401 points at a Keychain login",
			creds:        credentials{token: &ClaudeAiOAuth{AccessToken: "env"}, source: envOAuthToken},
			status:       http.StatusUnauthorized,
			wantRequests: 1,
			wantErr:      ErrTokenExpired,
			wantMsg:      "claude logout && claude login",
		},
		{
			name:         "env token with user:profile returns usage",
			creds:        credentials{token: &ClaudeAiOAuth{AccessToken: "env"}, source: envOAuthToken},
			status:       http.StatusOK,
			wantRequests: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				assert.Equal(t, "Bearer "+tt.creds.token.AccessToken, r.Header.Get("Authorization"))
				w.WriteHeader(tt.status)
				if tt.status == http.StatusOK {
					_, _ = w.Write([]byte(`{"five_hour":{"utilization":42,"resets_at":"2026-10-21T15:00:00Z"}}`))
				} else {
					_, _ = w.Write([]byte(`{"error":{"type":"authentication_error","message":"nope"}}`))
				}
			}))
			defer srv.Close()

			client := &Client{credentials: tt.creds, httpClient: srv.Client(), endpoint: srv.URL}
			usage, err := client.FetchUsage()

			assert.Equal(t, tt.wantRequests, requests)
			if tt.wantErr == nil {
				require.NoError(t, err)
				assert.InDelta(t, 42, usage.FiveHour.Utilisation, 0.001)
				return
			}
			assert.ErrorIs(t, err, tt.wantErr)
			if tt.wantMsg != "" {
				assert.Contains(t, err.Error(), tt.wantMsg)
			}
		})
	}
}
