package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// fallbackClaudeCodeVersion is used when the installed `claude` binary can't
// be queried (not on PATH, exec failed, unparseable output). Bump this during
// CCU releases so fresh installs don't identify themselves as a years-old
// client until their first successful `claude --version` call.
const fallbackClaudeCodeVersion = "2.1.114"

// versionCacheTTL controls how often we re-run `claude --version`. The check
// itself takes hundreds of milliseconds on Node-based binaries, so doing it
// every startup would be felt. Three days is long enough to keep startup snappy
// but short enough to pick up Claude Code updates within a working week.
const versionCacheTTL = 72 * time.Hour

type cachedVersion struct {
	Version    string    `json:"version"`
	DetectedAt time.Time `json:"detected_at"`
}

var (
	userAgentOnce sync.Once
	userAgentStr  string
)

// userAgent returns the User-Agent string to use for OAuth requests. The
// underlying version lookup runs at most once per CCU process, and is itself
// skipped when a recent value is cached on disk.
func userAgent() string {
	userAgentOnce.Do(func() {
		userAgentStr = fmt.Sprintf("claude-code/%s", detectClaudeCodeVersion())
	})
	return userAgentStr
}

// detectClaudeCodeVersion consults an on-disk cache first; if missing or
// stale, invokes `claude --version`, caches the result, and returns it.
// Guaranteed to return a non-empty string (falls back to a compiled-in value).
//
// Only successful detections are cached. If `claude --version` fails we use
// the compiled-in fallback but do NOT persist it, so that a subsequent install
// of Claude Code is picked up on the next start rather than after the full TTL.
func detectClaudeCodeVersion() string {
	cachePath := versionCachePath()
	if cachePath != "" {
		if v, ok := readVersionCache(cachePath); ok {
			return v
		}
	}

	if v := queryClaudeVersion(); v != "" {
		if cachePath != "" {
			_ = writeVersionCache(cachePath, v) // best-effort
		}
		return v
	}
	return fallbackClaudeCodeVersion
}

// versionCachePath returns the absolute path to the version cache file, or
// "" if the user's cache dir isn't available (in which case we skip caching).
func versionCachePath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "ccu", "claude-version.json")
}

func readVersionCache(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var c cachedVersion
	if err := json.Unmarshal(data, &c); err != nil {
		return "", false
	}
	if c.Version == "" || time.Since(c.DetectedAt) > versionCacheTTL {
		return "", false
	}
	return c.Version, true
}

func writeVersionCache(path, version string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(cachedVersion{
		Version:    version,
		DetectedAt: time.Now(),
	})
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// queryClaudeVersion runs `claude --version` with a short timeout and extracts
// the semver-ish string from its output. Returns "" on any failure.
func queryClaudeVersion() string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "claude", "--version").Output()
	if err != nil {
		return ""
	}
	return parseClaudeVersion(string(out))
}

// versionRE matches the leading semver-ish version in outputs like
// "2.1.114 (Claude Code)", "claude-code/2.1.114", or a bare "1.0.5".
var versionRE = regexp.MustCompile(`\d+\.\d+\.\d+(?:[.-][0-9A-Za-z]+)*`)

func parseClaudeVersion(s string) string {
	return versionRE.FindString(strings.TrimSpace(s))
}
