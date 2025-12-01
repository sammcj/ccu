package data

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/sammcj/ccu/internal/models"
	"github.com/sammcj/ccu/internal/pricing"
)

// RawEntry represents the actual Claude Code JSONL structure
type RawEntry struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	RequestID string `json:"requestId"`
	Message   struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// ParseJSONLLine parses a single JSONL line into a UsageEntry
func ParseJSONLLine(line []byte) (*models.UsageEntry, error) {
	if len(line) == 0 {
		return nil, fmt.Errorf("empty line")
	}

	var raw RawEntry
	if err := json.Unmarshal(line, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	// Skip non-assistant messages and file snapshots
	if raw.Type != "assistant" {
		return nil, nil // Expected skip, not an error
	}

	// Check if we have usage data
	if raw.Message.Usage.InputTokens == 0 && raw.Message.Usage.OutputTokens == 0 {
		return nil, nil // Expected skip, not an error
	}

	// Parse timestamp
	timestamp, err := parseTimestamp(raw.Timestamp)
	if err != nil {
		return nil, fmt.Errorf("timestamp parse error: %w", err)
	}

	entry := &models.UsageEntry{
		Timestamp:           timestamp,
		InputTokens:         raw.Message.Usage.InputTokens,
		OutputTokens:        raw.Message.Usage.OutputTokens,
		CacheCreationTokens: raw.Message.Usage.CacheCreationInputTokens,
		CacheReadTokens:     raw.Message.Usage.CacheReadInputTokens,
		Model:               raw.Message.Model,
		MessageID:           raw.Message.ID,
		RequestID:           raw.RequestID,
	}

	// Calculate cost
	entry.CostUSD = pricing.CalculateCost(*entry)

	return entry, nil
}

// parseTimestamp tries to parse various timestamp formats
func parseTimestamp(ts string) (time.Time, error) {
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02T15:04:05.999999Z07:00",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, ts); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse timestamp: %s", ts)
}

// DeduplicateEntries removes duplicate entries using hash
func DeduplicateEntries(entries []models.UsageEntry) []models.UsageEntry {
	seen := make(map[string]bool)
	result := make([]models.UsageEntry, 0, len(entries))

	for _, entry := range entries {
		hash := entry.Hash()
		if !seen[hash] {
			seen[hash] = true
			result = append(result, entry)
		}
	}

	return result
}

// FilterByTime filters entries to only include those within hoursBack from now
func FilterByTime(entries []models.UsageEntry, hoursBack int) []models.UsageEntry {
	if hoursBack <= 0 {
		return entries
	}

	cutoff := time.Now().Add(-time.Duration(hoursBack) * time.Hour)
	result := make([]models.UsageEntry, 0, len(entries))

	for _, entry := range entries {
		if entry.Timestamp.After(cutoff) {
			result = append(result, entry)
		}
	}

	return result
}

// SortEntriesByTime sorts entries by timestamp (oldest first)
func SortEntriesByTime(entries []models.UsageEntry) []models.UsageEntry {
	sorted := make([]models.UsageEntry, len(entries))
	copy(sorted, entries)

	slices.SortFunc(sorted, func(a, b models.UsageEntry) int {
		return a.Timestamp.Compare(b.Timestamp)
	})

	return sorted
}
