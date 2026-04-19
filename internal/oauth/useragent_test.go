package oauth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestParseClaudeVersion(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"current format", "2.1.114 (Claude Code)", "2.1.114"},
		{"slash format", "claude-code/2.1.114", "2.1.114"},
		{"bare version", "1.0.5", "1.0.5"},
		{"leading whitespace", "   2.3.0\n", "2.3.0"},
		{"prerelease suffix", "2.1.114-beta.1", "2.1.114-beta.1"},
		{"no version", "claude: error", ""},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, parseClaudeVersion(tt.in))
		})
	}
}

// TestVersionCacheRoundTrip verifies the cache file format survives a
// write/read cycle and that freshness gating works as intended.
func TestVersionCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude-version.json")

	assert.NoError(t, writeVersionCache(path, "2.1.114"))

	got, ok := readVersionCache(path)
	assert.True(t, ok, "fresh cache should be accepted")
	assert.Equal(t, "2.1.114", got)
}

func TestVersionCacheRejectsStale(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude-version.json")

	// Write a cache entry dated well past the TTL.
	stale := cachedVersion{
		Version:    "1.0.0",
		DetectedAt: time.Now().Add(-versionCacheTTL - time.Hour),
	}
	data, err := json.Marshal(stale)
	assert.NoError(t, err)
	assert.NoError(t, os.WriteFile(path, data, 0o644))

	_, ok := readVersionCache(path)
	assert.False(t, ok, "stale cache should be rejected")
}

func TestVersionCacheRejectsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude-version.json")

	empty := cachedVersion{Version: "", DetectedAt: time.Now()}
	data, err := json.Marshal(empty)
	assert.NoError(t, err)
	assert.NoError(t, os.WriteFile(path, data, 0o644))

	_, ok := readVersionCache(path)
	assert.False(t, ok, "empty version should be rejected")
}

func TestVersionCacheMissingFile(t *testing.T) {
	_, ok := readVersionCache(filepath.Join(t.TempDir(), "does-not-exist.json"))
	assert.False(t, ok)
}
