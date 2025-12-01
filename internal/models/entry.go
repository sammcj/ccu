package models

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
)

// UsageEntry represents a single Claude API call from JSONL data
type UsageEntry struct {
	Timestamp           time.Time `json:"timestamp"`
	InputTokens         int       `json:"input_tokens"`
	OutputTokens        int       `json:"output_tokens"`
	CacheCreationTokens int       `json:"cache_creation_input_tokens"`
	CacheReadTokens     int       `json:"cache_read_input_tokens"`
	CostUSD             float64   `json:"cost_usd"`
	Model               string    `json:"model"`
	MessageID           string    `json:"message_id"`
	RequestID           string    `json:"request_id"`
}

// TotalTokens returns the sum of all token types
func (e *UsageEntry) TotalTokens() int {
	return e.InputTokens + e.OutputTokens + e.CacheCreationTokens + e.CacheReadTokens
}

// DisplayTokens returns only input + output tokens (matching Python UI display)
// Cache tokens are excluded from UI display to match Python implementation
func (e *UsageEntry) DisplayTokens() int {
	return e.InputTokens + e.OutputTokens
}

// Hash generates a unique hash for deduplication using message_id + request_id
func (e *UsageEntry) Hash() string {
	combined := fmt.Sprintf("%s:%s", e.MessageID, e.RequestID)
	hash := sha256.Sum256([]byte(combined))
	return fmt.Sprintf("%x", hash)
}

// NormaliseModelName standardises model names for consistent grouping
func NormaliseModelName(model string) string {
	// Convert to lowercase for case-insensitive matching
	modelLower := strings.ToLower(model)

	// Map various model names to standard names
	// Check for version-specific patterns first (4.5, then 3.5, then base versions)
	switch {
	case strings.Contains(modelLower, "opus"):
		if strings.Contains(modelLower, "4-5") || strings.Contains(modelLower, "4.5") {
			return "claude-opus-4-5"
		}
		if strings.Contains(modelLower, "3") {
			return "claude-3-opus"
		}
		return "claude-opus-4"
	case strings.Contains(modelLower, "sonnet"):
		if strings.Contains(modelLower, "4-5") || strings.Contains(modelLower, "4.5") {
			return "claude-sonnet-4-5"
		}
		if strings.Contains(modelLower, "3-5") || strings.Contains(modelLower, "3.5") {
			return "claude-3-5-sonnet"
		}
		if strings.Contains(modelLower, "4") {
			return "claude-sonnet-4"
		}
		return "claude-3-sonnet"
	case strings.Contains(modelLower, "haiku"):
		if strings.Contains(modelLower, "4-5") || strings.Contains(modelLower, "4.5") {
			return "claude-haiku-4-5"
		}
		if strings.Contains(modelLower, "3-5") || strings.Contains(modelLower, "3.5") {
			return "claude-3-5-haiku"
		}
		return "claude-3-haiku"
	default:
		return model
	}
}

// ModelStats tracks per-model statistics
type ModelStats struct {
	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int
	CostUSD             float64
	MessageCount        int
}

// TotalTokens returns sum of all token types for this model
func (ms *ModelStats) TotalTokens() int {
	return ms.InputTokens + ms.OutputTokens + ms.CacheCreationTokens + ms.CacheReadTokens
}

// DisplayTokens returns only input + output tokens (matching Python UI display)
func (ms *ModelStats) DisplayTokens() int {
	return ms.InputTokens + ms.OutputTokens
}
