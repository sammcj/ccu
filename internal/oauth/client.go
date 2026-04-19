package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Common OAuth errors
var (
	// ErrTokenExpired indicates the OAuth token has expired (usually auto-refreshed by Claude Code)
	ErrTokenExpired = errors.New("OAuth token expired - will retry automatically (run 'claude login' if this persists)")
	// ErrNetworkError indicates a transient network issue
	ErrNetworkError = errors.New("network error")
	// ErrRateLimited indicates the API returned 429 Too Many Requests
	ErrRateLimited = errors.New("rate limited by API")
)

// RateLimitError wraps ErrRateLimited with an optional Retry-After duration
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("rate limited by API (retry after %s)", e.RetryAfter)
	}
	return "rate limited by API"
}

func (e *RateLimitError) Unwrap() error {
	return ErrRateLimited
}

// ClaudeAiOAuth represents the OAuth structure within keychain credentials
type ClaudeAiOAuth struct {
	AccessToken      string   `json:"accessToken"`
	RefreshToken     string   `json:"refreshToken"`
	ExpiresAt        int64    `json:"expiresAt"` // Unix timestamp in milliseconds
	Scopes           []string `json:"scopes"`
	SubscriptionType *string  `json:"subscriptionType"`
	RateLimitTier    string   `json:"rateLimitTier"`
}

// DetectPlan returns the CCU plan name derived from keychain credentials.
// rateLimitTier is checked first (e.g. "default_claude_max_5x") as it
// distinguishes Max5 from Max20; subscriptionType is a coarser fallback.
// Returns an empty string when the plan cannot be determined.
func DetectPlan() string {
	creds, err := getKeychainCredentials()
	if err != nil {
		return ""
	}
	return planFromCredentials(creds)
}

func planFromCredentials(creds *ClaudeAiOAuth) string {
	switch strings.ToLower(creds.RateLimitTier) {
	case "default_claude_max_5x":
		return "max5"
	case "default_claude_max_20x":
		return "max20"
	case "default_claude_pro":
		return "pro"
	}
	if creds.SubscriptionType != nil {
		switch strings.ToLower(*creds.SubscriptionType) {
		case "pro":
			return "pro"
		}
	}
	return ""
}

// KeychainCredentials represents the full structure stored in macOS Keychain
type KeychainCredentials struct {
	ClaudeAiOAuth ClaudeAiOAuth `json:"claudeAiOauth"`
}

// UsageData represents the OAuth API response structure
type UsageData struct {
	FiveHour struct {
		Utilisation float64 `json:"utilization"`
		ResetsAt    string  `json:"resets_at"`
	} `json:"five_hour"`
	SevenDay struct {
		Utilisation float64 `json:"utilization"`
		ResetsAt    string  `json:"resets_at"`
	} `json:"seven_day"`
	SevenDaySonnet *struct {
		Utilisation float64 `json:"utilization"`
		ResetsAt    string  `json:"resets_at"`
	} `json:"seven_day_sonnet"`
	SevenDayOpus *struct {
		Utilisation float64 `json:"utilization"`
		ResetsAt    *string `json:"resets_at"`
	} `json:"seven_day_opus"`
	ExtraUsage *struct {
		IsEnabled    bool     `json:"is_enabled"`
		MonthlyLimit *float64 `json:"monthly_limit"`
		UsedCredits  *float64 `json:"used_credits"`
		Utilisation  *float64 `json:"utilization"`
	} `json:"extra_usage"`
	FetchedAt time.Time `json:"-"` // When this data was fetched (not from API)
}

// Client handles OAuth-based usage data fetching
type Client struct {
	httpClient *http.Client
	token      *ClaudeAiOAuth
}

// NewClient creates a new OAuth client
func NewClient() (*Client, error) {
	token, err := getKeychainCredentials()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve OAuth credentials: %w", err)
	}

	return &Client{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		token:      token,
	}, nil
}

// getKeychainCredentials retrieves OAuth credentials from macOS Keychain
func getKeychainCredentials() (*ClaudeAiOAuth, error) {
	cmd := exec.Command("security", "find-generic-password", "-s", "Claude Code-credentials", "-w")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("keychain access failed: %w", err)
	}

	var creds KeychainCredentials
	if err := json.Unmarshal(output, &creds); err != nil {
		return nil, fmt.Errorf("failed to parse credentials: %w", err)
	}

	// Validate token has required scopes
	hasProfileScope := slices.Contains(creds.ClaudeAiOAuth.Scopes, "user:profile")

	if !hasProfileScope {
		return nil, fmt.Errorf("OAuth token lacks required 'user:profile' scope. Try re-authenticating: claude logout && claude login")
	}

	return &creds.ClaudeAiOAuth, nil
}

// FetchUsage retrieves current usage data from Anthropic's OAuth API
func (c *Client) FetchUsage() (*UsageData, error) {
	endpoint := "https://api.anthropic.com/api/oauth/usage"

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set required headers for OAuth beta endpoint. Header selection matches
	// what Claude Code itself sends (verified via mitmproxy against a live
	// client on 2026-04-20). Accept-Encoding is intentionally omitted so the
	// Go http package can auto-negotiate gzip and transparently decompress.
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token.AccessToken))
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("User-Agent", userAgent())
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Read up to 4 KiB of the body so we can still produce a useful error
		// message even when the payload isn't valid JSON. Parsing is best-effort;
		// the raw snippet is kept as a fallback for diagnostics.
		rawBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))

		var errorBody map[string]any
		decodeErr := json.Unmarshal(rawBody, &errorBody)

		// Handle 429 rate limiting with Retry-After support
		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			return nil, &RateLimitError{RetryAfter: retryAfter}
		}

		// Any 401 is treated as an auth failure requiring user action. The API
		// uses a couple of shapes here:
		//   error.details.error_code == "token_expired"  (auto-refreshable)
		//   error.type == "authentication_error"         (credentials rejected)
		// Both map to ErrTokenExpired so the UI shows a single, actionable
		// message instead of a wall of JSON.
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, ErrTokenExpired
		}

		if decodeErr != nil {
			snippet := strings.TrimSpace(string(rawBody))
			if len(snippet) > 200 {
				snippet = snippet[:200] + "..."
			}
			return nil, fmt.Errorf("API returned status %d (body: %q)", resp.StatusCode, snippet)
		}
		// Prefer the server's own message field if it's present, otherwise
		// fall back to the decoded map so diagnostics aren't lost.
		if msg := extractErrorMessage(errorBody); msg != "" {
			return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, msg)
		}
		return nil, fmt.Errorf("API returned status %d: %v", resp.StatusCode, errorBody)
	}

	var usage UsageData
	if err := json.NewDecoder(resp.Body).Decode(&usage); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	usage.FetchedAt = time.Now()
	return &usage, nil
}

// IsAvailable checks if OAuth authentication is available and properly configured
func IsAvailable() bool {
	_, err := getKeychainCredentials()
	return err == nil
}

// ParseResetTime converts the API's reset time string to time.Time
func ParseResetTime(resetAt string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, resetAt)
}

// IsTransientError returns true if the error is likely transient (network issues, rate limits)
// and the operation should be retried later
func IsTransientError(err error) bool {
	if err == nil {
		return false
	}
	// Token expired is not transient - but will be retried after cooldown
	if errors.Is(err, ErrTokenExpired) {
		return false
	}
	// Rate limiting is transient
	if errors.Is(err, ErrRateLimited) {
		return true
	}

	// Prefer typed checks over substring matching where possible.
	// context.Canceled is intentionally excluded: a cancelled context means the
	// caller is shutting down and does not want a retry.
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, io.EOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && (netErr.Timeout() || isTemporary(netErr)) {
		return true
	}

	// Fallback substring match for wrapped errors that don't expose typed values.
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "network") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "connection") ||
		strings.Contains(errStr, "dial") ||
		strings.Contains(errStr, "eof") ||
		strings.Contains(errStr, "reset by peer")
}

// isTemporary reports whether a net.Error considers itself transient. The
// Temporary() method is deprecated on most stdlib error types but still set on
// a handful of wrappers, so guard the assertion instead of relying on it.
func isTemporary(err error) bool {
	type temporary interface{ Temporary() bool }
	if t, ok := err.(temporary); ok {
		return t.Temporary()
	}
	return false
}

// extractErrorMessage pulls a human-readable message out of Anthropic's error
// envelope: { "error": { "message": "...", "type": "..." }, ... }. Falls back
// to the type if message is missing. Returns "" when neither is available.
func extractErrorMessage(body map[string]any) string {
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		return ""
	}
	if msg, ok := errObj["message"].(string); ok && msg != "" {
		if t, ok := errObj["type"].(string); ok && t != "" {
			return fmt.Sprintf("%s (%s)", msg, t)
		}
		return msg
	}
	if t, ok := errObj["type"].(string); ok {
		return t
	}
	return ""
}

// parseRetryAfter parses the Retry-After header value.
// Supports both delay-seconds (e.g. "60") and HTTP-date formats
// (RFC 7231 IMF-fixdate and the legacy RFC 850 / ANSI C asctime variants
// understood by http.ParseTime).
// Returns a default of 2 minutes if the header is missing or unparseable.
func parseRetryAfter(header string) time.Duration {
	const defaultRetry = 2 * time.Minute

	if header == "" {
		return defaultRetry
	}

	// Try as seconds first
	if seconds, err := strconv.Atoi(strings.TrimSpace(header)); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}

	// Try as HTTP-date. http.ParseTime covers IMF-fixdate, RFC 850 and asctime.
	if t, err := http.ParseTime(header); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d
		}
	}

	return defaultRetry
}

// GetRetryAfter extracts the RetryAfter duration from a RateLimitError.
// Returns 0 if the error is not a RateLimitError.
func GetRetryAfter(err error) time.Duration {
	var rle *RateLimitError
	if errors.As(err, &rle) {
		return rle.RetryAfter
	}
	return 0
}
