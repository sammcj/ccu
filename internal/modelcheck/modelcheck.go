// Package modelcheck compares ccu's model pricing and display-name tables
// against an upstream rates dataset so drift surfaces as an actionable report
// rather than silently wrong cost figures.
package modelcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/sammcj/ccu/internal/models"
	"github.com/sammcj/ccu/internal/pricing"
	"github.com/sammcj/ccu/internal/ui"
)

// UpstreamURL is the community-maintained LiteLLM pricing dataset. Anthropic's
// own Models API does not expose pricing, so this is the closest machine-readable
// upstream source of per-token rates.
const UpstreamURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

// tolerance for per-1M-token USD comparisons; differences below this are
// floating point noise, not pricing changes
const tolerance = 0.001

// ancientPrefixes are pre-Claude-3 model families that predate ccu entirely
// and are not worth flagging even if upstream still lists them.
var ancientPrefixes = []string{"claude-2", "claude-instant"}

type upstreamModel struct {
	InputCostPerToken           float64 `json:"input_cost_per_token"`
	OutputCostPerToken          float64 `json:"output_cost_per_token"`
	CacheCreationCostPerToken   float64 `json:"cache_creation_input_token_cost"`
	CacheReadCostPerToken       float64 `json:"cache_read_input_token_cost"`
	CacheCreation1hCostPerToken float64 `json:"cache_creation_input_token_cost_above_1hr"`
	Provider                    string  `json:"litellm_provider"`
	Mode                        string  `json:"mode"`
	DeprecationDate             string  `json:"deprecation_date"`
}

// Finding describes one way ccu's tables disagree with upstream.
// Model is the canonical (normalised) name an agent would add or fix.
type Finding struct {
	Model string
	Issue string
}

// Report summarises a comparison run.
type Report struct {
	Checked  int
	Findings []Finding
}

// FetchUpstream downloads the upstream pricing dataset from url
// (normally UpstreamURL; parameterised for testing).
func FetchUpstream(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching upstream pricing: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading upstream response: %w", err)
	}
	return body, nil
}

// Compare parses the upstream dataset and checks every current Anthropic chat
// model against ccu's normalisation, pricing, and display-name tables.
func Compare(data []byte) (*Report, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing upstream dataset: %w", err)
	}

	report := &Report{}
	seen := make(map[string]bool) // dedupe by normalised model + issue kind

	ids := make([]string, 0, len(raw))
	for id := range raw {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		var um upstreamModel
		if err := json.Unmarshal(raw[id], &um); err != nil {
			continue // metadata entries like sample_spec
		}

		name := strings.TrimPrefix(id, "anthropic/")
		if !isCurrentAnthropicChatModel(name, um) {
			continue
		}
		report.Checked++

		normalised := models.NormaliseModelName(name)
		local, ok := pricing.ModelPricing[normalised]
		if !ok {
			addFinding(report, seen, normalised, "missing-pricing",
				fmt.Sprintf("no pricing entry (silently billed at the Sonnet fallback rate); upstream model %q", name))
		} else {
			comparePricing(report, seen, normalised, name, local, um)
			if local.LongContext != nil {
				compareLongContext(report, seen, normalised, name, local, raw[id])
			}
		}

		if ui.FormatModelNameSimple(name) == name {
			addFinding(report, seen, normalised, "display-name",
				fmt.Sprintf("no friendly display name (UI shows raw %q)", name))
		}
	}

	return report, nil
}

// isCurrentAnthropicChatModel filters the upstream dataset down to the models
// ccu could plausibly see in usage data.
func isCurrentAnthropicChatModel(name string, um upstreamModel) bool {
	if um.Provider != "anthropic" || um.Mode != "chat" {
		return false
	}
	if !strings.HasPrefix(name, "claude-") {
		return false
	}
	for _, prefix := range ancientPrefixes {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}
	if um.DeprecationDate != "" {
		if t, err := time.Parse("2006-01-02", um.DeprecationDate); err == nil && t.Before(time.Now()) {
			return false
		}
	}
	return true
}

// comparePricing flags per-rate differences between local and upstream values.
// Upstream zero values are skipped: they mean the dataset is missing the field
// (common for cache rates on older models), not that the rate is free.
func comparePricing(report *Report, seen map[string]bool, normalised, upstreamID string, local pricing.Pricing, um upstreamModel) {
	rates := []struct {
		label    string
		local    float64
		upstream float64 // per token; converted to per-1M below
	}{
		{"input", local.Input, um.InputCostPerToken},
		{"output", local.Output, um.OutputCostPerToken},
		{"cache write", local.CacheCreation, um.CacheCreationCostPerToken},
		{"cache read", local.CacheRead, um.CacheReadCostPerToken},
		{"1h cache write", local.CacheCreation1h(), um.CacheCreation1hCostPerToken},
	}

	for _, r := range rates {
		compareRate(report, seen, normalised, upstreamID, r.label, r.local, r.upstream)
	}
}

// compareLongContext checks a prompt-length tier's upper rates against
// LiteLLM's "<field>_above_<N>k_tokens" variants, which vary by threshold and
// so are read from the raw entry.
func compareLongContext(report *Report, seen map[string]bool, normalised, upstreamID string, local pricing.Pricing, entry json.RawMessage) {
	var fields map[string]any
	if err := json.Unmarshal(entry, &fields); err != nil {
		return
	}
	k := local.LongContextThreshold / 1000
	suffix := fmt.Sprintf("_above_%dk_tokens", k)
	upper := local.LongContext
	rates := []struct {
		label, field string
		local        float64
	}{
		{"input", "input_cost_per_token", upper.Input},
		{"output", "output_cost_per_token", upper.Output},
		{"cache write", "cache_creation_input_token_cost", upper.CacheCreation},
		{"cache read", "cache_read_input_token_cost", upper.CacheRead},
		{"1h cache write", "cache_creation_input_token_cost_above_1hr", upper.CacheCreation1h()},
	}
	for _, r := range rates {
		upstream, _ := fields[r.field+suffix].(float64)
		compareRate(report, seen, normalised, upstreamID, fmt.Sprintf("over-%dk %s", k, r.label), r.local, upstream)
	}
}

// compareRate flags one rate that differs from upstream. Upstream zero values
// are skipped: they mean the dataset is missing the field, not that it's free.
func compareRate(report *Report, seen map[string]bool, normalised, upstreamID, label string, local, upstreamPerToken float64) {
	upstreamPerM := upstreamPerToken * 1_000_000
	if upstreamPerM == 0 {
		return
	}
	if math.Abs(upstreamPerM-local) > tolerance {
		addFinding(report, seen, normalised, "rate-"+label,
			fmt.Sprintf("%s rate is $%.2f/M locally but $%.2f/M upstream (model %q)",
				label, local, upstreamPerM, upstreamID))
	}
}

func addFinding(report *Report, seen map[string]bool, model, kind, issue string) {
	key := model + "|" + kind
	if seen[key] {
		return
	}
	seen[key] = true
	report.Findings = append(report.Findings, Finding{Model: model, Issue: issue})
}

const agentHint = `
Agent hint - a new version of an existing family (e.g. opus 6) needs NO normalisation
change: NormaliseModelName() parses versions generically. Only pricing needs updating.
  1. internal/pricing/pricing.go   ModelPricing: add or correct the canonical key (USD per 1M
                                   tokens; cache write is typically 1.25x input and cache read
                                   0.1x input unless upstream says otherwise). If the whole
                                   family repriced, update FamilyPricing too - that is the
                                   fallback for versions with no explicit entry.
  2. A brand new FAMILY (not just a version) additionally needs:
     - internal/models/entry.go    ModelFamilies + latestFamilyVersion
     - internal/pricing/pricing.go FamilyPricing
     - internal/ui/styles.go       GetModelColour() + a ColorX constant
     FormatModelNameSimple() reads ModelFamilies, so it needs no separate edit.
  3. Add matching test cases in internal/models/entry_test.go, internal/pricing/pricing_test.go
     and internal/ui/dashboard_test.go
Verify with: make test && ./ccu -check-models
`

// Format renders the report for terminal output, including an agent hint when
// anything needs changing.
func (r *Report) Format() string {
	var b strings.Builder
	if len(r.Findings) == 0 {
		fmt.Fprintf(&b, "OK: ccu's model tables are in sync with upstream (%d models checked)\n", r.Checked)
		return b.String()
	}

	fmt.Fprintf(&b, "DRIFT: %d issue(s) found across %d upstream models:\n\n", len(r.Findings), r.Checked)
	for _, f := range r.Findings {
		fmt.Fprintf(&b, "  %-22s %s\n", f.Model, f.Issue)
	}
	b.WriteString(agentHint)
	return b.String()
}
