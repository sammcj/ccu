package pricing

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
| Claude Haiku 4.5 | $1 / MTok | TBC | $2 / MTok | $0.10 / MTok | $5 / MTok |
| Claude Sonnet 5 | $0.002 / KTok | $0.0025 / KTok | $0.004 / KTok | $0.0002 / KTok | $0.01 / KTok |
| Claude Sonnet 4.6 | ~~$5 / MTok~~ $3 / MTok | $3.75 / MTok | $6 / MTok | $0.30 / MTok | $15 / MTok |
| Some Other Model | $1 / MTok | $1 / MTok | $1 / MTok | $1 / MTok | $1 / MTok |

Footnotes follow.

| Model | Base input tokens | 5m cache writes | 1h cache writes | Cache hits and refreshes | Output tokens |
| :---- | :---------------- | :-------------- | :-------------- | :----------------------- | :------------ |
| Claude Opus 9 | $1 / MTok | $1 / MTok | $1 / MTok | $1 / MTok | $1 / MTok |
`

func TestParsePublished(t *testing.T) {
	got := parsePublished(pageFixture)

	assert.Equal(t, map[string]Pricing{
		"claude-fable-5-1": {Input: 10, Output: 50, CacheCreation: 12.50, CacheRead: 0.25},
		"claude-opus-5-5":  {Input: 4, Output: 20, CacheCreation: 5, CacheRead: 0.20},
		"claude-opus-6":    {Input: 3, Output: 15, CacheCreation: 3.75, CacheRead: 0.30},
		"claude-opus-4":    {Input: 15, Output: 75, CacheCreation: 18.75, CacheRead: 1.50},
	}, got, "unparseable rows, other units, non-Claude rows and later tables are skipped")
}

func TestParsePublishedNoTable(t *testing.T) {
	assert.Empty(t, parsePublished("# Pricing\n\n| Plan | Price |\n| --- | --- |\n| Pro | $20 |\n"))
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
	p.enable(url, cachePath)
	return p
}

func TestPublishedLookup(t *testing.T) {
	opus55 := Pricing{Input: 4, Output: 20, CacheCreation: 5, CacheRead: 0.20}

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
		got, ok = newPublished(srv.URL, cachePath).lookup("claude-opus-5-5")
		require.True(t, ok)
		assert.Equal(t, opus55, got)
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
	assert.Equal(t, Pricing{Input: 3, Output: 15, CacheCreation: 3.75, CacheRead: 0.30}, got)
	assert.Nil(t, EstimatedModels([]string{"claude-opus-6"}), "a published rate is not an estimate")

	// Built-in rates win over the page
	got, _ = Lookup("claude-opus-4")
	assert.Equal(t, ModelPricing["claude-opus-4"], got)

	// Still unpublished: family estimate as before
	_, source = Lookup("claude-opus-9")
	assert.Equal(t, SourceFamily, source)
}
