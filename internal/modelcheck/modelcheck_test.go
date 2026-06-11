package modelcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixture mirrors the LiteLLM dataset shape: a sample_spec metadata entry with
// non-numeric fields, plus per-model entries keyed by model ID.
const fixtureInSync = `{
	"sample_spec": {"input_cost_per_token": "float", "litellm_provider": "one of...", "mode": "chat"},
	"claude-sonnet-4-6": {
		"input_cost_per_token": 0.000003,
		"output_cost_per_token": 0.000015,
		"cache_creation_input_token_cost": 0.00000375,
		"cache_read_input_token_cost": 0.0000003,
		"litellm_provider": "anthropic",
		"mode": "chat"
	},
	"claude-fable-5": {
		"input_cost_per_token": 0.00001,
		"output_cost_per_token": 0.00005,
		"cache_creation_input_token_cost": 0.0000125,
		"cache_read_input_token_cost": 0.000001,
		"litellm_provider": "anthropic",
		"mode": "chat"
	},
	"gpt-4": {
		"input_cost_per_token": 0.00003,
		"litellm_provider": "openai",
		"mode": "chat"
	},
	"vertex_ai/claude-sonnet-4-6": {
		"input_cost_per_token": 0.000099,
		"litellm_provider": "vertex_ai",
		"mode": "chat"
	},
	"claude-2.1": {
		"input_cost_per_token": 0.000008,
		"litellm_provider": "anthropic",
		"mode": "chat"
	},
	"claude-3-7-sonnet-20250219": {
		"input_cost_per_token": 0.000099,
		"litellm_provider": "anthropic",
		"mode": "chat",
		"deprecation_date": "2026-02-19"
	}
}`

func TestCompareInSync(t *testing.T) {
	report, err := Compare([]byte(fixtureInSync))
	require.NoError(t, err)

	// Only the two current anthropic chat models count: gpt-4 (wrong provider),
	// the vertex variant (wrong provider), claude-2.1 (ancient), and the
	// deprecated 3.7 sonnet are all filtered out
	assert.Equal(t, 2, report.Checked)
	assert.Empty(t, report.Findings)
	assert.Contains(t, report.Format(), "in sync")
}

func TestCompareUnknownModel(t *testing.T) {
	fixture := `{
		"claude-zenith-1": {
			"input_cost_per_token": 0.00002,
			"output_cost_per_token": 0.0001,
			"litellm_provider": "anthropic",
			"mode": "chat"
		}
	}`
	report, err := Compare([]byte(fixture))
	require.NoError(t, err)

	require.Len(t, report.Findings, 2)
	assert.Equal(t, "claude-zenith-1", report.Findings[0].Model)
	assert.Contains(t, report.Findings[0].Issue, "no pricing entry")
	assert.Contains(t, report.Findings[1].Issue, "no friendly display name")

	out := report.Format()
	assert.Contains(t, out, "Agent hint")
	assert.Contains(t, out, "internal/pricing/pricing.go")
}

func TestCompareRateMismatch(t *testing.T) {
	fixture := `{
		"claude-opus-4-8": {
			"input_cost_per_token": 0.000007,
			"output_cost_per_token": 0.000025,
			"litellm_provider": "anthropic",
			"mode": "chat"
		}
	}`
	report, err := Compare([]byte(fixture))
	require.NoError(t, err)

	require.Len(t, report.Findings, 1)
	assert.Equal(t, "claude-opus-4-8", report.Findings[0].Model)
	assert.Contains(t, report.Findings[0].Issue, "input rate is $5.00/M locally but $7.00/M upstream")
}

func TestCompareDedupesDatedVariants(t *testing.T) {
	fixture := `{
		"claude-opus-4-8": {
			"input_cost_per_token": 0.000007,
			"litellm_provider": "anthropic",
			"mode": "chat"
		},
		"claude-opus-4-8-20260301": {
			"input_cost_per_token": 0.000007,
			"litellm_provider": "anthropic",
			"mode": "chat"
		}
	}`
	report, err := Compare([]byte(fixture))
	require.NoError(t, err)

	// Both variants normalise to claude-opus-4-8; the same issue reports once
	assert.Equal(t, 2, report.Checked)
	require.Len(t, report.Findings, 1)
}

func TestCompareSkipsMissingUpstreamCacheRates(t *testing.T) {
	// Upstream zero cache fields mean missing data, not a free rate
	fixture := `{
		"claude-haiku-4-5": {
			"input_cost_per_token": 0.000001,
			"output_cost_per_token": 0.000005,
			"litellm_provider": "anthropic",
			"mode": "chat"
		}
	}`
	report, err := Compare([]byte(fixture))
	require.NoError(t, err)
	assert.Empty(t, report.Findings)
}

func TestCompareInvalidJSON(t *testing.T) {
	_, err := Compare([]byte("not json"))
	assert.Error(t, err)
}

func TestFetchUpstream(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"ok": true}`))
		}))
		defer srv.Close()

		data, err := FetchUpstream(context.Background(), srv.URL)
		require.NoError(t, err)
		assert.JSONEq(t, `{"ok": true}`, string(data))
	})

	t.Run("non-200 response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		_, err := FetchUpstream(context.Background(), srv.URL)
		assert.ErrorContains(t, err, "HTTP 500")
	})

	t.Run("unreachable host", func(t *testing.T) {
		_, err := FetchUpstream(context.Background(), "http://127.0.0.1:0/nope")
		assert.Error(t, err)
	})
}

func TestFormatDriftOutput(t *testing.T) {
	report := &Report{
		Checked: 5,
		Findings: []Finding{
			{Model: "claude-test-1", Issue: "no pricing entry"},
		},
	}
	out := report.Format()
	assert.True(t, strings.HasPrefix(out, "DRIFT:"))
	assert.Contains(t, out, "claude-test-1")
	assert.Contains(t, out, "NormaliseModelName")
	assert.Contains(t, out, "make test && ./ccu -check-models")
}
