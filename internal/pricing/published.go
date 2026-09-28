package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sammcj/ccu/internal/models"
)

// publishedURL serves Anthropic's pricing page as markdown.
const publishedURL = "https://platform.claude.com/docs/en/about-claude/pricing.md"

const (
	publishedTTL     = 24 * time.Hour
	publishedTimeout = 5 * time.Second
	maxPublishedSize = 2 << 20
)

// published fills gaps in ModelPricing from Anthropic's pricing page, so a model
// released after this build is costed at its real rate rather than its family's.
// It stays disabled until EnablePublished is called, which keeps tests and the
// model table check off the network.
var published = &publishedRates{}

// EnablePublished lets Lookup consult Anthropic's pricing page for models missing
// from ModelPricing. Fetched rates are cached under the user's cache dir.
func EnablePublished() {
	cachePath := ""
	if dir, err := os.UserCacheDir(); err == nil {
		cachePath = filepath.Join(dir, "ccu", "pricing.json")
	}
	// Newly published models also become what a bare family name ("sonnet") means
	published.enable(publishedURL, cachePath, models.AdoptNewerVersions)
	go published.refreshIfStale()
}

type publishedRates struct {
	// fetchMu serialises the network fetch; mu guards the fields below and is
	// never held across the fetch, so the UI's lookups of already-known models
	// don't wait on a slow request.
	fetchMu   sync.Mutex
	mu        sync.Mutex
	enabled   bool
	url       string
	cachePath string
	fetched   bool // network fetch attempted this process
	fetchedAt time.Time
	rates     map[string]Pricing
	released  []string
	onRates   func(released []string) // told the released models whenever rates load
}

// publishedPage is what the pricing page yields: rates for every listed model,
// and which of those are released without a qualifier such as "limited
// availability" or "retired". Only released models may become what a bare
// family name means.
type publishedPage struct {
	Rates    map[string]Pricing `json:"rates"`
	Released []string           `json:"released"`
}

type publishedCache struct {
	FetchedAt time.Time `json:"fetched_at"`
	publishedPage
}

// enable reads the disk cache straight away rather than on the first miss, so
// onRates hears about cached models before any usage data is normalised.
func (p *publishedRates) enable(url, cachePath string, onRates func([]string)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.enabled, p.url, p.cachePath, p.onRates = true, url, cachePath, onRates
	p.readCache()
}

// lookup returns the published rate for a normalised model name. A fresh disk
// cache that has the model answers without a request. Otherwise the page is
// fetched at most once per process, so a model Anthropic hasn't published yet
// doesn't cost a request on every lookup.
func (p *publishedRates) lookup(model string) (Pricing, bool) {
	if rate, ok, settled := p.cached(model); settled {
		return rate, ok
	}

	p.fetchMu.Lock()
	defer p.fetchMu.Unlock()
	// Another caller may have fetched while this one waited
	if rate, ok, settled := p.cached(model); settled {
		return rate, ok
	}

	// On failure a stale cached rate still beats the family estimate
	p.fetch()
	rate, ok, _ := p.cached(model)
	return rate, ok
}

// refreshIfStale fetches the page when the cache is missing or stale, so new
// releases reach bare family names even when every model in use is already in
// ModelPricing and no lookup ever misses.
func (p *publishedRates) refreshIfStale() {
	p.fetchMu.Lock()
	defer p.fetchMu.Unlock()
	p.mu.Lock()
	stale := p.enabled && !p.fetched && time.Since(p.fetchedAt) >= publishedTTL
	p.mu.Unlock()
	if stale {
		p.fetch()
	}
}

// fetch must be called with fetchMu held.
func (p *publishedRates) fetch() {
	page, err := fetchPublished(p.url)

	p.mu.Lock()
	defer p.mu.Unlock()
	p.fetched = true
	if err != nil {
		log.Printf("pricing: fetching published rates: %v", err)
		return
	}
	p.setRates(page, time.Now())
	p.writeCache()
}

// cached answers from memory or the disk cache. settled is false when the
// answer could improve with a fetch: the model is missing or its rate is stale,
// and this process hasn't fetched yet.
func (p *publishedRates) cached(model string) (rate Pricing, ok, settled bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.enabled {
		return Pricing{}, false, true
	}
	rate, ok = p.rates[model]
	fresh := ok && time.Since(p.fetchedAt) < publishedTTL
	return rate, ok, fresh || p.fetched
}

func (p *publishedRates) readCache() {
	if p.cachePath == "" {
		return
	}
	data, err := os.ReadFile(p.cachePath)
	if err != nil {
		return
	}
	var c publishedCache
	if err := json.Unmarshal(data, &c); err != nil {
		return
	}
	p.setRates(c.publishedPage, c.FetchedAt)
}

// setRates must be called with mu held, so onRates must not call back into
// this package.
func (p *publishedRates) setRates(page publishedPage, fetchedAt time.Time) {
	p.rates, p.released, p.fetchedAt = page.Rates, page.Released, fetchedAt
	if p.onRates != nil {
		p.onRates(page.Released)
	}
}

func (p *publishedRates) writeCache() {
	if p.cachePath == "" {
		return
	}
	data, err := json.Marshal(publishedCache{
		FetchedAt:     p.fetchedAt,
		publishedPage: publishedPage{Rates: p.rates, Released: p.released},
	})
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p.cachePath), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(p.cachePath, data, 0o644); err != nil {
		log.Printf("pricing: writing %s: %v", p.cachePath, err)
	}
}

func fetchPublished(url string) (publishedPage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), publishedTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return publishedPage{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return publishedPage{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return publishedPage{}, fmt.Errorf("unexpected status %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPublishedSize))
	if err != nil {
		return publishedPage{}, err
	}
	page := parsePublished(string(body))
	if len(page.Rates) == 0 {
		return publishedPage{}, errors.New("no model pricing table found - page layout may have changed")
	}
	return page, nil
}

// dollarPattern only accepts per-million-token rates; a cell in any other unit
// drops the row rather than caching a wrong rate as exact.
var dollarPattern = regexp.MustCompile(`^\$([0-9]+(?:\.[0-9]+)?)\s*/\s*MTok\b`)

// parsePublished reads the model pricing table from the pricing page markdown,
// keyed by normalised model name. Columns are found by header text so a new
// column doesn't shift the rates.
func parsePublished(md string) publishedPage {
	page := publishedPage{Rates: make(map[string]Pricing)}
	var cols map[string]int
	for line := range strings.SplitSeq(md, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			if cols != nil {
				break // only the first model table
			}
			continue
		}
		cells := splitRow(line)
		if cols == nil {
			cols = pricingColumns(cells)
			continue
		}
		model, qualified := modelFromDisplayName(cells[0])
		if model == "" {
			continue
		}
		if rate, ok := parseRow(cells, cols); ok {
			page.Rates[model] = rate
			if !qualified {
				page.Released = append(page.Released, model)
			}
		}
	}
	return page
}

func splitRow(line string) []string {
	cells := strings.Split(strings.Trim(line, "|"), "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

// pricingColumns maps each rate to its column index, or returns nil when the
// row isn't the model pricing table's header.
func pricingColumns(header []string) map[string]int {
	want := []string{"base input tokens", "5m cache writes", "cache hits and refreshes", "output tokens"}
	cols := make(map[string]int)
	for i, cell := range header {
		cols[strings.ToLower(cell)] = i
	}
	for _, name := range want {
		if _, ok := cols[name]; !ok {
			return nil
		}
	}
	return cols
}

func parseRow(cells []string, cols map[string]int) (Pricing, bool) {
	var vals [4]float64
	for i, name := range []string{"base input tokens", "output tokens", "5m cache writes", "cache hits and refreshes"} {
		idx := cols[name]
		if idx >= len(cells) {
			return Pricing{}, false
		}
		m := dollarPattern.FindStringSubmatch(cells[idx])
		if m == nil {
			return Pricing{}, false
		}
		v, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return Pricing{}, false
		}
		vals[i] = v
	}
	return Pricing{Input: vals[0], Output: vals[1], CacheCreation: vals[2], CacheRead: vals[3]}, true
}

// modelFromDisplayName turns "Claude Opus 5.5" or "Claude Opus 4 ([retired](...))"
// into the normalised key Lookup uses, and reports whether the name carried a
// parenthesised qualifier. Separator rows and non-Claude names give "".
func modelFromDisplayName(name string) (model string, qualified bool) {
	name, _, qualified = strings.Cut(name, "(")
	name = strings.ToLower(strings.TrimSpace(name))
	if !strings.HasPrefix(name, "claude ") {
		return "", false
	}
	id := strings.NewReplacer(" ", "-", ".", "-").Replace(name)
	normalised := models.NormaliseModelName(id)
	if models.FamilyOf(normalised) == "" {
		return "", false
	}
	return normalised, qualified
}
