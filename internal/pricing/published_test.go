package pricing

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pageFixture mirrors the shape of Anthropic's pricing page markdown: footnote
// markers, link suffixes on retired models, and a later table with the same
// header that must be ignored.
const pageFixture = `# Pricing

## Model pricing

| Model | Base input tokens | 5m cache writes | 1h cache writes | Cache hits and refreshes | Output tokens |
| :---- | :---------------- | :-------------- | :-------------- | :----------------------- | :------------ |
| Claude Fable 5.1 | $10 / MTok | $12.50 / MTok | $20 / MTok | $0.25 / MTok<sup>1</sup> | $50 / MTok |
| Claude Opus 5.5 | $4 / MTok | $5 / MTok | $8 / MTok | $0.20 / MTok<sup>2</sup> | $20 / MTok |
| Claude Opus 6 | $3 / MTok | $3.75 / MTok | $6 / MTok | $0.30 / MTok | $15 / MTok |
| Claude Opus 4 ([retired, except on Google Cloud](https://example.com)) | $15 / MTok | $18.75 / MTok | $30 / MTok | $1.50 / MTok | $75 / MTok |
| Claude Opus 7 ([limited availability](https://example.com)) | $20 / MTok | $25 / MTok | $40 / MTok | $2 / MTok | $100 / MTok |
| Claude Haiku 4.5 | $1 / MTok | TBC | $2 / MTok | $0.10 / MTok | $5 / MTok |
| Claude Haiku 6 (for prompts up to 100,000 tokens) | $0.10 / MTok | $0.125 / MTok | $0.20 / MTok | $0.01 / MTok | $0.50 / MTok |
| Claude Haiku 6 (for prompts over 100,000 tokens) | $0.50 / MTok | $0.625 / MTok | $1 / MTok | $0.05 / MTok | $2.50 / MTok |
| Claude Sonnet 6 (for prompts over 200,000 tokens) | $6 / MTok | $7.50 / MTok | $12 / MTok | $0.60 / MTok | $30 / MTok |
| Claude Sonnet 5 | $0.002 / KTok | $0.0025 / KTok | $0.004 / KTok | $0.0002 / KTok | $0.01 / KTok |
| Claude Sonnet 4.6 | ~~$5 / MTok~~ $3 / MTok | $3.75 / MTok | $6 / MTok | $0.30 / MTok | $15 / MTok |
| Some Other Model | $1 / MTok | $1 / MTok | $1 / MTok | $1 / MTok | $1 / MTok |

Footnotes follow.

### Fast mode pricing

| Model | Input | Output |
| ----- | ----- | ------ |
| Claude Opus 5.5 | $8 / MTok | $40 / MTok |
| Claude Opus 6 / Claude Opus 4 | $6 / MTok | $30 / MTok |
| Claude Fable 5.1 | $15 / MTok | $60 / MTok |
| Claude Opus 9 | $2 / MTok | $2 / MTok |

| Model | Base input tokens | 5m cache writes | 1h cache writes | Cache hits and refreshes | Output tokens |
| :---- | :---------------- | :-------------- | :-------------- | :----------------------- | :------------ |
| Claude Opus 9 | $1 / MTok | $1 / MTok | $1 / MTok | $1 / MTok | $1 / MTok |
`

func TestParsePublished(t *testing.T) {
	got := parsePublished(pageFixture)

	assert.Equal(t, map[string]Pricing{
		"claude-fable-5-1": {Input: 10, Output: 50, CacheCreation: 12.50, CacheRead: 0.25},
		"claude-opus-5-5":  {Input: 4, Output: 20, CacheCreation: 5, CacheRead: 0.20, FastMultiplier: 2},
		"claude-opus-6":    {Input: 3, Output: 15, CacheCreation: 3.75, CacheRead: 0.30, FastMultiplier: 2},
		"claude-opus-4":    {Input: 15, Output: 75, CacheCreation: 18.75, CacheRead: 1.50},
		"claude-opus-7":    {Input: 20, Output: 100, CacheCreation: 25, CacheRead: 2},
		"claude-haiku-6": {
			Input: 0.10, Output: 0.50, CacheCreation: 0.125, CacheRead: 0.01,
			LongContextThreshold: 100_000,
			LongContext:          &Pricing{Input: 0.50, Output: 2.50, CacheCreation: 0.625, CacheRead: 0.05},
		},
	}, got.Rates, "unparseable rows, other units, non-Claude rows, incomplete tiers and later tables are skipped; "+
		"fast mode applies only as a premium on both input and output")
	assert.Equal(t, []string{"claude-fable-5-1", "claude-opus-5-5", "claude-opus-6", "claude-haiku-6"}, got.Released,
		"retired and limited availability models are priced but not released; prompt-size tiers are released")
}

// TestParsePublishedTierEdgeCases covers page layouts where trusting any one row
// would cache a rate that is wrong on one side of the threshold.
func TestParsePublishedTierEdgeCases(t *testing.T) {
	const header = "| Model | Base input tokens | 5m cache writes | 1h cache writes | Cache hits and refreshes | Output tokens |\n| --- | --- | --- | --- | --- | --- |\n"
	const rates = " | $1 / MTok | $1.25 / MTok | $2 / MTok | $0.10 / MTok | $5 / MTok |\n"
	page := parsePublished(header +
		"| Claude Sonnet 7" + rates +
		"| Claude Sonnet 7 (for prompts over 200,000 tokens)" + rates +
		"| Claude Haiku 7 (for prompts up to 100K tokens)" + rates +
		"| Claude Haiku 7 (for prompts over 100K tokens)" + rates +
		"| Claude Opus 8 ([limited availability](https://example.com)) (for prompts up to 100,000 tokens)" + rates +
		"| Claude Opus 8 ([limited availability](https://example.com)) (for prompts over 100,000 tokens)" + rates)

	assert.NotContains(t, page.Rates, "claude-sonnet-7", "a flat row plus a lone upper tier is incomplete")
	assert.NotContains(t, page.Released, "claude-sonnet-7")
	assert.NotContains(t, page.Rates, "claude-haiku-7", "unrecognised tier wording")
	assert.Contains(t, page.Rates, "claude-opus-8", "limited availability models are still priced")
	assert.NotContains(t, page.Released, "claude-opus-8", "a tier note doesn't hide another qualifier")
}

// TestBuiltinFastMultipliersMatchPage pins the hand-coded fast mode premiums to
// the pricing page's fast mode table, copied verbatim (2026-10-09). Built-in
// rates win over the page, so a repricing would otherwise go unnoticed.
func TestBuiltinFastMultipliersMatchPage(t *testing.T) {
	const header = "| Model | Base input tokens | 5m cache writes | 1h cache writes | Cache hits and refreshes | Output tokens |\n| --- | --- | --- | --- | --- | --- |\n"
	models := map[string]string{
		"claude-opus-5-5": "Opus 5.5", "claude-opus-5": "Opus 5",
		"claude-opus-4-8": "Opus 4.8", "claude-opus-4-6": "Opus 4.6",
	}
	var rows strings.Builder
	for key, name := range models {
		p := ModelPricing[key]
		fmt.Fprintf(&rows, "| Claude %s | $%g / MTok | $%g / MTok | $1 / MTok | $%g / MTok | $%g / MTok |\n",
			name, p.Input, p.CacheCreation, p.CacheRead, p.Output)
	}
	const fastSection = "\n### Fast mode pricing\n\n" +
		"```bash\n# not a heading\n```\n\n" +
		"| Model                           | Input      | Output     |\n" +
		"| ------------------------------- | ---------- | ---------- |\n" +
		"| Claude Opus 5.5                 | $8 / MTok  | $40 / MTok |\n" +
		"| Claude Opus 5 / Claude Opus 4.8 | $10 / MTok | $50 / MTok |\n"

	page := parsePublished(header + rows.String() + fastSection)
	for key := range models {
		require.Contains(t, page.Rates, key)
		assert.Equal(t, ModelPricing[key].FastMultiplier, page.Rates[key].FastMultiplier, key)
	}
}

func TestParsePublishedNoTable(t *testing.T) {
	assert.Empty(t, parsePublished("# Pricing\n\n| Plan | Price |\n| --- | --- |\n| Pro | $20 |\n").Rates)
}

// pageServer serves pageFixture and counts requests.
func pageServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(pageFixture))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func newPublished(url, cachePath string) *publishedRates {
	p := &publishedRates{}
	p.enable(url, cachePath, nil)
	return p
}

func TestPublishedReportsModelNames(t *testing.T) {
	srv, _ := pageServer(t)
	cachePath := filepath.Join(t.TempDir(), "pricing.json")
	var heard []string
	record := func(names []string) { heard = names }

	// Fetch: a miss loads the page and reports its released models
	p := &publishedRates{}
	p.enable(srv.URL, cachePath, record)
	assert.Empty(t, heard, "no cache yet")
	p.lookup("claude-opus-6")
	assert.Equal(t, []string{"claude-fable-5-1", "claude-opus-5-5", "claude-opus-6", "claude-haiku-6"}, heard)

	// Cache: a later run reports the cached models at enable, before any lookup
	heard = nil
	(&publishedRates{}).enable(srv.URL, cachePath, record)
	assert.Contains(t, heard, "claude-opus-6")
}

func TestRefreshIfStale(t *testing.T) {
	t.Run("missing cache fetches once", func(t *testing.T) {
		srv, hits := pageServer(t)
		p := newPublished(srv.URL, filepath.Join(t.TempDir(), "pricing.json"))
		p.refreshIfStale()
		p.refreshIfStale()
		assert.EqualValues(t, 1, hits.Load())
		_, ok := p.lookup("claude-opus-6")
		assert.True(t, ok)
		assert.EqualValues(t, 1, hits.Load(), "lookup reuses the refreshed rates")
	})

	t.Run("fresh cache does not fetch", func(t *testing.T) {
		srv, hits := pageServer(t)
		cachePath := filepath.Join(t.TempDir(), "pricing.json")
		(&publishedRates{cachePath: cachePath, fetchedAt: time.Now()}).writeCache()
		newPublished(srv.URL, cachePath).refreshIfStale()
		assert.Zero(t, hits.Load())
	})

	t.Run("disabled does not fetch", func(t *testing.T) {
		srv, hits := pageServer(t)
		(&publishedRates{url: srv.URL}).refreshIfStale()
		assert.Zero(t, hits.Load())
	})
}

func TestPublishedLookup(t *testing.T) {
	opus55 := Pricing{Input: 4, Output: 20, CacheCreation: 5, CacheRead: 0.20, FastMultiplier: 2}

	t.Run("disabled never fetches", func(t *testing.T) {
		srv, hits := pageServer(t)
		p := &publishedRates{url: srv.URL}
		_, ok := p.lookup("claude-opus-5-5")
		assert.False(t, ok)
		assert.Zero(t, hits.Load())
	})

	t.Run("fetches once, then serves later runs from the cache", func(t *testing.T) {
		srv, hits := pageServer(t)
		cachePath := filepath.Join(t.TempDir(), "ccu", "pricing.json")

		got, ok := newPublished(srv.URL, cachePath).lookup("claude-opus-5-5")
		require.True(t, ok)
		assert.Equal(t, opus55, got)
		assert.FileExists(t, cachePath)

		// A new process reads the fresh cache without a request
		later := newPublished(srv.URL, cachePath)
		got, ok = later.lookup("claude-opus-5-5")
		require.True(t, ok)
		assert.Equal(t, opus55, got)
		tiered, ok := later.lookup("claude-haiku-6")
		require.True(t, ok)
		assert.Equal(t, parsePublished(pageFixture).Rates["claude-haiku-6"], tiered, "tiers survive the cache")
		assert.EqualValues(t, 1, hits.Load())
	})

	t.Run("unpublished model fetches at most once per process", func(t *testing.T) {
		srv, hits := pageServer(t)
		p := newPublished(srv.URL, filepath.Join(t.TempDir(), "pricing.json"))

		for range 3 {
			_, ok := p.lookup("claude-opus-9")
			assert.False(t, ok)
		}
		assert.EqualValues(t, 1, hits.Load())
	})

	t.Run("stale cache is used when the fetch fails", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(srv.Close)
		cachePath := filepath.Join(t.TempDir(), "pricing.json")
		stale := &publishedRates{
			cachePath: cachePath,
			fetchedAt: time.Now().Add(-2 * publishedTTL),
			rates:     map[string]Pricing{"claude-opus-5-5": opus55},
		}
		stale.writeCache()

		got, ok := newPublished(srv.URL, cachePath).lookup("claude-opus-5-5")
		require.True(t, ok)
		assert.Equal(t, opus55, got)
	})

	t.Run("stale cache is refreshed by a successful fetch", func(t *testing.T) {
		srv, hits := pageServer(t)
		cachePath := filepath.Join(t.TempDir(), "pricing.json")
		old := Pricing{Input: 99}
		stale := &publishedRates{
			cachePath: cachePath,
			fetchedAt: time.Now().Add(-2 * publishedTTL),
			rates:     map[string]Pricing{"claude-opus-5-5": old},
		}
		stale.writeCache()

		got, ok := newPublished(srv.URL, cachePath).lookup("claude-opus-5-5")
		require.True(t, ok)
		assert.Equal(t, opus55, got)
		assert.EqualValues(t, 1, hits.Load())

		reread := &publishedRates{cachePath: cachePath}
		reread.readCache()
		assert.Equal(t, opus55, reread.rates["claude-opus-5-5"])
		assert.WithinDuration(t, time.Now(), reread.fetchedAt, time.Minute)
	})

	t.Run("fresh cache missing the model fetches once", func(t *testing.T) {
		srv, hits := pageServer(t)
		cachePath := filepath.Join(t.TempDir(), "pricing.json")
		fresh := &publishedRates{
			cachePath: cachePath,
			fetchedAt: time.Now(),
			rates:     map[string]Pricing{"claude-opus-5-5": opus55},
		}
		fresh.writeCache()

		p := newPublished(srv.URL, cachePath)
		_, ok := p.lookup("claude-opus-6")
		assert.True(t, ok)
		_, ok = p.lookup("claude-opus-5-5")
		assert.True(t, ok)
		assert.EqualValues(t, 1, hits.Load())
	})

	t.Run("known model does not wait on an in-flight fetch", func(t *testing.T) {
		release := make(chan struct{})
		started := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			close(started)
			<-release
			_, _ = w.Write([]byte(pageFixture))
		}))
		t.Cleanup(srv.Close)
		t.Cleanup(func() { close(release) })

		cachePath := filepath.Join(t.TempDir(), "pricing.json")
		fresh := &publishedRates{
			cachePath: cachePath,
			fetchedAt: time.Now(),
			rates:     map[string]Pricing{"claude-opus-5-5": opus55},
		}
		fresh.writeCache()

		p := newPublished(srv.URL, cachePath)
		go p.lookup("claude-opus-9") // missing, so it fetches and blocks
		<-started

		done := make(chan struct{})
		go func() {
			_, ok := p.lookup("claude-opus-5-5")
			assert.True(t, ok)
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("lookup of a cached model blocked on the fetch")
		}
	})

	t.Run("corrupt cache falls through to a fetch", func(t *testing.T) {
		srv, hits := pageServer(t)
		cachePath := filepath.Join(t.TempDir(), "pricing.json")
		require.NoError(t, os.WriteFile(cachePath, []byte("not json"), 0o644))

		_, ok := newPublished(srv.URL, cachePath).lookup("claude-opus-5-5")
		assert.True(t, ok)
		assert.EqualValues(t, 1, hits.Load())
	})
}

func TestLookupUsesPublishedRates(t *testing.T) {
	srv, _ := pageServer(t)
	orig := published
	published = newPublished(srv.URL, filepath.Join(t.TempDir(), "pricing.json"))
	t.Cleanup(func() { published = orig })

	got, source := Lookup("claude-opus-6[1m]")
	assert.Equal(t, SourceExact, source)
	assert.Equal(t, Pricing{Input: 3, Output: 15, CacheCreation: 3.75, CacheRead: 0.30, FastMultiplier: 2}, got)
	assert.Nil(t, EstimatedModels([]string{"claude-opus-6"}), "a published rate is not an estimate")

	// Built-in rates win over the page
	got, _ = Lookup("claude-opus-4")
	assert.Equal(t, ModelPricing["claude-opus-4"], got)

	// Still unpublished: family estimate as before
	_, source = Lookup("claude-opus-9")
	assert.Equal(t, SourceFamily, source)
}
