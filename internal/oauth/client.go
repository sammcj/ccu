package oauth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"time"
)

// ClaudeAiOAuth represents the OAuth structure within keychain credentials
type ClaudeAiOAuth struct {
	AccessToken      string   `json:"accessToken"`
	RefreshToken     string   `json:"refreshToken"`
	ExpiresAt        int64    `json:"expiresAt"` // Unix timestamp in milliseconds
	Scopes           []string `json:"scopes"`
	SubscriptionType *string  `json:"subscriptionType"`
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
	hasProfileScope := false
	for _, scope := range creds.ClaudeAiOAuth.Scopes {
		if scope == "user:profile" {
			hasProfileScope = true
			break
		}
	}

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

	// Set required headers for OAuth beta endpoint
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token.AccessToken))
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("User-Agent", "claude-code/2.0.54")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errorBody map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&errorBody)
		return nil, fmt.Errorf("API returned status %d: %v", resp.StatusCode, errorBody)
	}

	var usage UsageData
	if err := json.NewDecoder(resp.Body).Decode(&usage); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

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
