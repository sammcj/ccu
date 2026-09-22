package models

import (
	"regexp"
	"strconv"
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

// Hash returns the deduplication key for this entry. The message_id and
// request_id pair is already unique, so the key is a plain concatenation
// rather than a cryptographic hash - it only ever feeds an in-memory map.
func (e *UsageEntry) Hash() string {
	return e.MessageID + ":" + e.RequestID
}

// ModelFamilies lists every Claude family CCU understands, newest tier first.
// Adding a family here is the only change needed for CCU to name and group a new
// one - versions within a family are parsed generically by NormaliseModelName.
var ModelFamilies = []string{"fable", "mythos", "opus", "sonnet", "haiku"}

// latestFamilyVersion maps a family to its newest released version, used when a
// model string names a family with no version at all (Claude Code writes bare
// "opus" / "sonnet" / "fable" into some JSONL entries). Guessing the newest is
// the least-wrong option: an unversioned name always means "whatever the plan
// currently serves", never an older generation.
var latestFamilyVersion = map[string]string{
	"fable":  "5-1",
	"mythos": "5-1",
	"opus":   "5-5",
	"sonnet": "5",
	"haiku":  "4-5",
}

// familyVersionPattern captures the family and its version from a modern model
// ID, e.g. "claude-opus-4-8-20260101" → ("opus", "4-8"). Version components are
// capped at two digits and must end on a non-digit, which is what keeps a
// trailing date stamp out of the key: in "claude-opus-4-20250514" the parser
// cannot take "-20" as a minor version because a digit follows it, so the
// version is "4". Suffixes that are not digits at all ("[1m]") stop it anyway.
var familyVersionPattern = regexp.MustCompile(
	`(` + strings.Join(ModelFamilies, "|") + `)[-.]?(\d{1,2}(?:[-.]\d{1,2})?)(?:[^0-9]|$)`)

// versionFamilyPattern captures the same pair from the other ID shape Anthropic
// publishes, where the version precedes the family:
// "claude-3-5-sonnet-20241022" → ("sonnet", "3-5"). Both grammars are live -
// "claude-4-opus-20250514" is in the current upstream rates dataset - so treating
// this one as a Claude 3 relic mixes up two generations of Opus pricing.
var versionFamilyPattern = regexp.MustCompile(
	`claude[-.](\d{1,2}(?:[-.]\d{1,2})?)[-.](` + strings.Join(ModelFamilies, "|") + `)`)

// canonicalName renders a parsed (family, version) pair as CCU's lookup key.
// Anthropic moved the version after the family at Claude 4 and the pricing tables
// follow suit, so the two eras key differently: "claude-3-5-sonnet" but
// "claude-sonnet-4-5". The version decides the shape, never the ID it came from.
func canonicalName(family, version string) string {
	version = strings.ReplaceAll(version, ".", "-")
	major, _ := strconv.Atoi(strings.SplitN(version, "-", 2)[0])
	if major < 4 {
		return "claude-" + version + "-" + family
	}
	return "claude-" + family + "-" + version
}

// NormaliseModelName standardises model names for consistent grouping.
//
// Versions are parsed rather than enumerated, so a model released after this
// code was written still normalises to its own key: "claude-opus-5" becomes
// "claude-opus-5", not the family's oldest generation. Getting this wrong is
// expensive - the normalised name is the pricing table's lookup key, so a
// mis-normalised model is silently billed at another model's rate.
func NormaliseModelName(model string) string {
	modelLower := strings.ToLower(model)

	if m := familyVersionPattern.FindStringSubmatch(modelLower); m != nil {
		return canonicalName(m[1], m[2])
	}
	if m := versionFamilyPattern.FindStringSubmatch(modelLower); m != nil {
		return canonicalName(m[2], m[1])
	}

	// A family name with no version at all - resolve to the newest release.
	if family := FamilyOf(modelLower); family != "" {
		return "claude-" + family + "-" + latestFamilyVersion[family]
	}

	return model
}

// FamilyOf returns the ModelFamilies entry a model name belongs to, or "" when
// it matches none (non-Claude models, synthetic entries).
func FamilyOf(model string) string {
	modelLower := strings.ToLower(model)
	for _, family := range ModelFamilies {
		if strings.Contains(modelLower, family) {
			return family
		}
	}
	return ""
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
