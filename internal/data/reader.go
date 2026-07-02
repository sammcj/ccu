package data

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sammcj/ccu/internal/models"
)

// jsonlFile carries the stat data captured during the directory walk so the
// fingerprint and cache-validation stages don't re-stat every file per tick.
type jsonlFile struct {
	path  string
	mtime time.Time
	size  int64
}

// fileCacheEntry is one file's parsed output. Entries are stored pre-dedupe so
// the merge step can apply first-seen-wins deduplication across files in a
// stable order regardless of which files were re-parsed this tick.
type fileCacheEntry struct {
	mtime   time.Time
	size    int64
	entries []models.UsageEntry
}

// loadCache memoises LoadUsageData across refresh ticks, per file. Unchanged
// files (same mtime and size) reuse their previously parsed entries, so a
// growing live JSONL file only costs its own reparse rather than the whole
// window. When nothing changed at all, the previous merged slice is returned
// unchanged - internal/app relies on slice identity to skip recomputation.
type loadCache struct {
	mu          sync.Mutex
	hoursBack   int
	fingerprint uint64
	valid       bool
	files       map[string]fileCacheEntry
	merged      []models.UsageEntry
}

var globalLoadCache loadCache

// findJSONLFiles recursively finds all .jsonl files under rootPath, capturing
// mtime and size from the directory entry so each file is only stat'd once.
func findJSONLFiles(rootPath string) ([]jsonlFile, error) {
	var files []jsonlFile

	err := filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Skip directories we can't read
			return nil
		}

		// Include all .jsonl files (matches Python implementation)
		if d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), ".jsonl") {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			// File vanished between listing and stat; skip it
			return nil
		}

		files = append(files, jsonlFile{path: path, mtime: info.ModTime(), size: info.Size()})
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walking directory tree: %w", err)
	}

	return files, nil
}

// maxJSONLLineBytes is the largest single JSONL line the scanner will accept.
// Some Claude Code recordings include large tool-use payloads on a single line.
// A var rather than a const so tests can lower it without writing 10 MiB files.
var maxJSONLLineBytes = 10 * 1024 * 1024 // 10 MiB

// readStats records non-fatal problems encountered while loading JSONL data.
// Values are advisory: callers use them to surface a single summary warning
// rather than logging every skipped file.
//
//   - skippedFiles: files that couldn't be opened OR whose scan aborted
//     partway (bubbled up from readJSONLFileWithFilter as an error). Entries
//     parsed before an aborted scan are still kept.
//   - parseErrors: individual JSONL lines that failed to unmarshal. The file
//     they came from still contributes any lines that did parse.
type readStats struct {
	skippedFiles int
	parseErrors  int
	lastErr      error
}

// readJSONLFileWithFilter reads a JSONL file with time filtering and deduplication.
// cutoff: entries before this time are skipped (zero value = no filter).
// seen: map for deduplication (nil = no deduplication).
// scanBuf: scanner working buffer shared across files; can grow up to maxJSONLLineBytes.
// stats: aggregates non-fatal counters (parse errors, scan errors).
func readJSONLFileWithFilter(filePath string, cutoff time.Time, seen map[string]bool, scanBuf []byte, stats *readStats) ([]models.UsageEntry, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("opening file: %w", err)
	}
	defer file.Close()

	var entries []models.UsageEntry
	scanner := bufio.NewScanner(file)
	scanner.Buffer(scanBuf, maxJSONLLineBytes)

	hasCutoff := !cutoff.IsZero()
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()

		// Skip empty lines
		if len(line) == 0 {
			continue
		}

		entry, err := ParseJSONLLine(line)
		if err != nil {
			if stats != nil {
				stats.parseErrors++
				stats.lastErr = fmt.Errorf("%s:%d: %w", filePath, lineNum, err)
			}
			continue
		}

		// Skip nil entries (expected skips from parser)
		if entry == nil {
			continue
		}

		// Apply time filter during parse
		if hasCutoff && entry.Timestamp.Before(cutoff) {
			continue
		}

		// Apply deduplication during parse
		if seen != nil {
			hash := entry.Hash()
			if seen[hash] {
				continue
			}
			seen[hash] = true
		}

		entries = append(entries, *entry)
	}

	if err := scanner.Err(); err != nil {
		// Scan failure (e.g. a single line beyond maxJSONLLineBytes) bubbles up
		// as a file-level error, but the entries parsed before the failure are
		// returned so callers keep the file's valid data.
		return entries, fmt.Errorf("scanning file %s: %w", filePath, err)
	}

	return entries, nil
}

// GetDefaultDataPath returns the default Claude data path
func GetDefaultDataPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}

	claudePath := filepath.Join(homeDir, ".claude", "projects")

	// Check if path exists
	if _, err := os.Stat(claudePath); os.IsNotExist(err) {
		return "", fmt.Errorf("claude data directory not found: %s", claudePath)
	}

	return claudePath, nil
}

// LoadUsageData loads all usage data within the hoursBack window, reusing
// per-file parse results from the previous call where files are unchanged.
func LoadUsageData(dataPath string, hoursBack int) ([]models.UsageEntry, error) {
	// Use default path if not specified
	if dataPath == "" {
		var err error
		dataPath, err = GetDefaultDataPath()
		if err != nil {
			return nil, err
		}
	}

	files, err := findJSONLFiles(dataPath)
	if err != nil {
		return nil, fmt.Errorf("finding JSONL files: %w", err)
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no JSONL files found in %s", dataPath)
	}

	// Deterministic order so first-seen-wins dedupe is stable across ticks.
	slices.SortFunc(files, func(a, b jsonlFile) int { return strings.Compare(a.path, b.path) })

	var cutoff time.Time
	if hoursBack > 0 {
		cutoff = time.Now().Add(-time.Duration(hoursBack) * time.Hour)
	}
	fingerprint := loadFingerprint(files, cutoff)

	c := &globalLoadCache
	c.mu.Lock()
	defer c.mu.Unlock()

	// Fast path: same window, no file changed. Return the previous merged
	// slice itself (not a copy) - internal/app compares slice identity to skip
	// session recomputation, so the backing array must be pointer-identical.
	if c.valid && c.hoursBack == hoursBack && c.fingerprint == fingerprint {
		return c.merged, nil
	}

	// An hoursBack change can move the cutoff backwards, and per-file entries
	// were parse-time filtered against the old cutoff, so none can be reused.
	if c.files == nil || c.hoursBack != hoursBack {
		c.files = make(map[string]fileCacheEntry)
	}

	// Reuse one scanner buffer across every file rather than allocating a fresh
	// 10 MiB per file; bufio.Scanner grows it up to maxJSONLLineBytes on demand.
	scanBuf := make([]byte, 64*1024)
	stats := &readStats{}
	next := make(map[string]fileCacheEntry, len(files))
	seen := make(map[string]bool)
	var merged []models.UsageEntry
	retryNeeded := false

	for _, f := range files {
		// Files older than the window contribute nothing; leaving them out of
		// next also evicts deleted and aged-out files from the cache.
		if !cutoff.IsZero() && f.mtime.Before(cutoff) {
			continue
		}

		ce, ok := c.files[f.path]
		if !ok || !ce.mtime.Equal(f.mtime) || ce.size != f.size {
			// New or changed file: reparse without dedupe (dedupe happens at
			// merge). A scan failure keeps the entries parsed before it.
			entries, err := readJSONLFileWithFilter(f.path, cutoff, nil, scanBuf, stats)
			if err != nil {
				stats.skippedFiles++
				stats.lastErr = err
				if len(entries) == 0 {
					// Nothing usable (e.g. a transient open failure): leave the
					// file uncached and invalidate the fast path so the next
					// load retries it rather than treating it as empty forever.
					retryNeeded = true
					continue
				}
			}
			ce = fileCacheEntry{mtime: f.mtime, size: f.size, entries: entries}
		}
		next[f.path] = ce

		// Merge with first-seen-wins dedupe. The cutoff moves forward every
		// call, so cached entries are re-filtered here (cheap) instead of
		// forcing a reparse when only the window boundary moved.
		for i := range ce.entries {
			e := &ce.entries[i]
			if !cutoff.IsZero() && e.Timestamp.Before(cutoff) {
				continue
			}
			key := e.Hash()
			if seen[key] {
				continue
			}
			seen[key] = true
			merged = append(merged, *e)
		}
	}

	if stats.skippedFiles > 0 || stats.parseErrors > 0 {
		log.Printf("data: %d file(s) skipped, %d parse error(s); last err: %v",
			stats.skippedFiles, stats.parseErrors, stats.lastErr)
	}

	// Sort by timestamp, oldest first
	slices.SortFunc(merged, func(a, b models.UsageEntry) int {
		return a.Timestamp.Compare(b.Timestamp)
	})

	c.files = next
	c.hoursBack = hoursBack
	c.fingerprint = fingerprint
	c.merged = merged
	c.valid = !retryNeeded

	return merged, nil
}

// loadFingerprint produces a signature of the JSONL files that would be merged
// for this window: path, mtime and size of every file inside the cutoff. Files
// whose mtime is already before the cutoff are excluded because the load skips
// them anyway. A running FNV-64 avoids building the previous implementation's
// large concatenated string every tick.
func loadFingerprint(files []jsonlFile, cutoff time.Time) uint64 {
	h := fnv.New64a()
	var buf [8]byte
	for _, f := range files {
		if !cutoff.IsZero() && f.mtime.Before(cutoff) {
			continue
		}
		h.Write([]byte(f.path))
		h.Write([]byte{0})
		binary.LittleEndian.PutUint64(buf[:], uint64(f.mtime.UnixNano()))
		h.Write(buf[:])
		binary.LittleEndian.PutUint64(buf[:], uint64(f.size))
		h.Write(buf[:])
	}
	return h.Sum64()
}
