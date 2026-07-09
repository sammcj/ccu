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
	"sync"
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
	// ErrMissingScope indicates the stored token lacks the user:profile scope;
	// only re-authenticating fixes this, so it must never auto-retry
	ErrMissingScope = errors.New("OAuth token lacks required 'user:profile' scope. Try re-authenticating: claude logout && claude login")
)

// keychainTimeout bounds the `security` subprocess so a locked keychain's GUI
// prompt can't block the caller (and the TUI) indefinitely.
const keychainTimeout = 5 * time.Second

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

// Limit kinds reported in the API's `limits` array.
const (
	KindSession      = "session"
	KindWeeklyAll    = "weekly_all"
	KindWeeklyScoped = "weekly_scoped"
)

// LimitModel identifies the model a limit applies to. `id` is frequently null
// while `display_name` (e.g. "Fable") is always populated.
type LimitModel struct {
	DisplayName string  `json:"display_name"`
	ID          *string `json:"id"`
}

// LimitScope narrows a limit to a particular model and/or surface.
// Both fields are nil on unscoped (account-wide) limits.
type LimitScope struct {
	Model   *LimitModel `json:"model"`
	Surface *string     `json:"surface"`
}

// Limit is one entry of the API's self-describing `limits` array. Anthropic
// added this array to express per-model limits (Fable's weekly cap being the
// first) without minting a new top-level field per model, so a limit for a
// future model arrives here needing no change on our side.
//
// IsActive marks the limit currently binding the account rather than one being
// enforced: a live 5-hour session and an enforced Fable weekly cap both report
// is_active=false while the highest weekly bucket reports true. Do not read it
// as "this limit applies".
type Limit struct {
	Group    string      `json:"group"`
	Kind     string      `json:"kind"`
	Percent  float64     `json:"percent"`
	ResetsAt *string     `json:"resets_at"`
	Scope    *LimitScope `json:"scope"`
	Severity string      `json:"severity"`
	IsActive bool        `json:"is_active"`
}

// ModelName returns the display name of the model this limit is scoped to,
// or an empty string when the limit is not model-scoped.
func (l Limit) ModelName() string {
	if l.Scope == nil || l.Scope.Model == nil {
		return ""
	}
	return l.Scope.Model.DisplayName
}

// SurfaceName returns the surface this limit is narrowed to (e.g. "web"), or an
// empty string when it applies across all surfaces. Anthropic returns null here
// today, but the field exists so a limit can be scoped to a model on one surface.
func (l Limit) SurfaceName() string {
	if l.Scope == nil || l.Scope.Surface == nil {
		return ""
	}
	return *l.Scope.Surface
}

// Key uniquely identifies a scoped limit. Model name alone is not unique: the
// same model can carry separate limits per surface, and keying on the model
// would silently collapse them into one.
func (l Limit) Key() string {
	key := strings.ToLower(l.ModelName())
	if surface := l.SurfaceName(); surface != "" {
		key += "/" + strings.ToLower(surface)
	}
	return key
}

// Label renders the limit's scope for display, e.g. "Fable" or "Fable (web)".
func (l Limit) Label() string {
	if surface := l.SurfaceName(); surface != "" {
		return fmt.Sprintf("%s (%s)", l.ModelName(), surface)
	}
	return l.ModelName()
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
	// Limits supersedes the SevenDaySonnet/SevenDayOpus fields below, which
	// Anthropic now returns as null. Read it via WeeklyModelLimits.
	Limits         []Limit `json:"limits"`
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

// WeeklyModelLimits returns the weekly limits scoped to an individual model,
// sorted by Key so bar ordering stays stable across refreshes.
//
// Prefers the self-describing `limits` array. When that is absent (older API
// responses) it synthesises equivalent entries from the legacy
// seven_day_sonnet / seven_day_opus fields, so callers get one uniform list
// either way and never branch on which source produced it. The fallback is
// all-or-nothing: we assume Anthropic nulls the legacy fields once it populates
// `limits`, as it does today. A response carrying both would show only the
// `limits` entries.
func (u *UsageData) WeeklyModelLimits() []Limit {
	scoped := make([]Limit, 0, len(u.Limits))
	for _, l := range u.Limits {
		if l.Kind == KindWeeklyScoped && l.ModelName() != "" {
			scoped = append(scoped, l)
		}
	}

	if len(scoped) == 0 {
		scoped = u.legacyWeeklyModelLimits()
	}

	slices.SortStableFunc(scoped, func(a, b Limit) int {
		return strings.Compare(a.Key(), b.Key())
	})
	return scoped
}

// legacyWeeklyModelLimits maps the pre-`limits` response shape onto []Limit.
// Opus is only included when it carries a reset time: that was the signal
// Anthropic used to indicate the Opus weekly cap was actually being enforced.
func (u *UsageData) legacyWeeklyModelLimits() []Limit {
	var out []Limit
	if u.SevenDaySonnet != nil {
		resetsAt := u.SevenDaySonnet.ResetsAt
		out = append(out, newScopedWeeklyLimit("Sonnet", u.SevenDaySonnet.Utilisation, &resetsAt))
	}
	if u.SevenDayOpus != nil && u.SevenDayOpus.ResetsAt != nil {
		out = append(out, newScopedWeeklyLimit("Opus", u.SevenDayOpus.Utilisation, u.SevenDayOpus.ResetsAt))
	}
	return out
}

func newScopedWeeklyLimit(displayName string, percent float64, resetsAt *string) Limit {
	return Limit{
		Group:    "weekly",
		Kind:     KindWeeklyScoped,
		Percent:  percent,
		ResetsAt: resetsAt,
		Scope:    &LimitScope{Model: &LimitModel{DisplayName: displayName}},
		Severity: "normal",
	}
}

// EffectiveFiveHour returns the five-hour utilisation adjusted for session rollover.
// When ResetsAt has passed the window has rolled over: resetsAt is advanced by
// 5 hours and the reported utilisation is checked for plausibility. A new
// session's utilisation can be at most (elapsed / 5h) * 100 (floored at 1%);
// anything more than double that is stale data from the previous window, so
// percent is clamped to 0 and stale is true until the API catches up.
// An unparseable ResetsAt yields the raw utilisation with stale=false.
func (u *UsageData) EffectiveFiveHour(now time.Time) (percent float64, resetsAt time.Time, stale bool) {
	percent = u.FiveHour.Utilisation
	resetsAt, _ = ParseResetTime(u.FiveHour.ResetsAt)

	if resetsAt.After(now) {
		return percent, resetsAt, false
	}

	// The new window started at the old reset time
	sessionStart := resetsAt
	resetsAt = resetsAt.Add(5 * time.Hour)

	elapsed := now.Sub(sessionStart)
	maxReasonablePercent := (elapsed.Hours() / 5.0) * 100
	if maxReasonablePercent < 1 {
		maxReasonablePercent = 1
	}
	if percent > maxReasonablePercent*2 {
		return 0, resetsAt, true
	}
	return percent, resetsAt, false
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
	ctx, cancel := context.WithTimeout(context.Background(), keychainTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "security", "find-generic-password", "-s", "Claude Code-credentials", "-w")
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
		return nil, ErrMissingScope
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

// Availability cache: IsAvailable is called on every refresh tick, and each
// uncached check forks a `security` subprocess (which can block on a keychain
// prompt). Both positive and negative results are cached for availabilityTTL.
const availabilityTTL = 60 * time.Second

var (
	availabilityMu    sync.Mutex
	availabilityAt    time.Time
	availabilityValue bool
	// checkKeychain is a seam for tests; production uses the real keychain lookup
	checkKeychain = func() bool {
		_, err := getKeychainCredentials()
		return err == nil
	}
)

// IsAvailable checks if OAuth authentication is available and properly configured.
// The result is cached for a short TTL so frequent callers don't fork a
// subprocess each time.
func IsAvailable() bool {
	availabilityMu.Lock()
	defer availabilityMu.Unlock()

	if !availabilityAt.IsZero() && time.Since(availabilityAt) < availabilityTTL {
		return availabilityValue
	}

	availabilityValue = checkKeychain()
	availabilityAt = time.Now()
	return availabilityValue
}

// RequiresUserAction reports whether the error can only be resolved by the user
// re-authenticating (claude logout && claude login). Token expiry is excluded
// because Claude Code usually refreshes the token itself, so it stays eligible
// for auto-retry.
func RequiresUserAction(err error) bool {
	return errors.Is(err, ErrMissingScope)
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
	if rle, ok := errors.AsType[*RateLimitError](err); ok {
		return rle.RetryAfter
	}
	return 0
}
