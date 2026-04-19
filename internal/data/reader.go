package data

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sammcj/ccu/internal/models"
)

// loadCache memoises LoadUsageData across refresh ticks. When the set of
// relevant files (and their mtime/size) is unchanged since the previous call,
// the cached slice is returned without touching disk. This is the hot path
// when the TUI is idle: ~2k JSONL files, ~200 of them active, parsed every
// refresh interval otherwise.
type loadCache struct {
	mu          sync.Mutex
	fingerprint string
	hoursBack   int
	entries     []models.UsageEntry
}

var globalLoadCache loadCache

// FindJSONLFiles recursively finds all .jsonl files in the given path
func FindJSONLFiles(rootPath string) ([]string, error) {
	var jsonlFiles []string

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Skip directories we can't read
			return nil
		}

		// Include all .jsonl files (matches Python implementation)
		if !info.IsDir() && strings.HasSuffix(strings.ToLower(info.Name()), ".jsonl") {
			jsonlFiles = append(jsonlFiles, path)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walking directory tree: %w", err)
	}

	return jsonlFiles, nil
}

// maxJSONLLineBytes is the largest single JSONL line the scanner will accept.
// Some Claude Code recordings include large tool-use payloads on a single line.
const maxJSONLLineBytes = 10 * 1024 * 1024 // 10 MiB

// readStats records non-fatal problems encountered while loading JSONL data.
// Values are advisory: callers use them to surface a single summary warning
// rather than logging every skipped file.
//
//   - skippedFiles: files that couldn't be opened OR whose scan aborted
//     partway (bubbled up from readJSONLFileWithFilter as an error).
//   - parseErrors: individual JSONL lines that failed to unmarshal. The file
//     they came from still contributes any lines that did parse.
type readStats struct {
	skippedFiles int
	parseErrors  int
	lastErr      error
}

// ReadUsageEntries reads usage entries from JSONL files
// Optimised to filter during parsing to reduce memory usage
func ReadUsageEntries(files []string, hoursBack int) ([]models.UsageEntry, error) {
	// Calculate cutoff time upfront
	var cutoff time.Time
	if hoursBack > 0 {
		cutoff = time.Now().Add(-time.Duration(hoursBack) * time.Hour)
	}

	// Use a map for deduplication during read to avoid storing duplicates
	seen := make(map[string]bool)
	var allEntries []models.UsageEntry

	// Reuse one scanner buffer across every file rather than allocating a fresh
	// 10 MiB per file. bufio.Scanner will grow from the initial size up to the
	// cap on demand, so this covers the "one huge line" case without the
	// per-file allocation cost when the tree has thousands of JSONL files.
	scanBuf := make([]byte, 64*1024)
	stats := &readStats{}

	for _, file := range files {
		// Check file modification time - skip old files entirely
		if hoursBack > 0 {
			info, err := os.Stat(file)
			if err == nil && info.ModTime().Before(cutoff) {
				// File hasn't been modified since cutoff, skip entirely
				continue
			}
		}

		entries, err := readJSONLFileWithFilter(file, cutoff, seen, scanBuf, stats)
		if err != nil {
			stats.skippedFiles++
			stats.lastErr = err
			continue
		}
		allEntries = append(allEntries, entries...)
	}

	if stats.skippedFiles > 0 || stats.parseErrors > 0 {
		log.Printf("data: %d file(s) skipped, %d parse error(s); last err: %v",
			stats.skippedFiles, stats.parseErrors, stats.lastErr)
	}

	// Sort by timestamp
	allEntries = SortEntriesByTime(allEntries)

	return allEntries, nil
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
		// Scan failure bubbles up as a file-level error; ReadUsageEntries will
		// increment skippedFiles. We don't add a separate counter here to avoid
		// double-counting the same failure under two different labels.
		return nil, fmt.Errorf("scanning file %s: %w", filePath, err)
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

// LoadUsageData is a convenience function to load all usage data
func LoadUsageData(dataPath string, hoursBack int) ([]models.UsageEntry, error) {
	// Use default path if not specified
	if dataPath == "" {
		var err error
		dataPath, err = GetDefaultDataPath()
		if err != nil {
			return nil, err
		}
	}

	// Find all JSONL files
	files, err := FindJSONLFiles(dataPath)
	if err != nil {
		return nil, fmt.Errorf("finding JSONL files: %w", err)
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no JSONL files found in %s", dataPath)
	}

	// Build a fingerprint of the files that would actually be parsed (inside
	// the cutoff window). If it matches the previous call, skip the reparse.
	fingerprint := buildLoadFingerprint(files, hoursBack)

	globalLoadCache.mu.Lock()
	if globalLoadCache.entries != nil &&
		globalLoadCache.hoursBack == hoursBack &&
		globalLoadCache.fingerprint == fingerprint {
		cached := globalLoadCache.entries
		globalLoadCache.mu.Unlock()
		return cached, nil
	}
	globalLoadCache.mu.Unlock()

	// Read and process entries
	entries, err := ReadUsageEntries(files, hoursBack)
	if err != nil {
		return nil, fmt.Errorf("reading usage entries: %w", err)
	}

	globalLoadCache.mu.Lock()
	globalLoadCache.fingerprint = fingerprint
	globalLoadCache.hoursBack = hoursBack
	globalLoadCache.entries = entries
	globalLoadCache.mu.Unlock()

	return entries, nil
}

// buildLoadFingerprint produces a deterministic signature of the JSONL files
// that will be parsed by ReadUsageEntries for this hoursBack window. Files
// whose mtime is already before the cutoff are excluded because
// ReadUsageEntries skips them anyway. Stat failures fall through as an empty
// row so a missing file still invalidates the cache.
func buildLoadFingerprint(files []string, hoursBack int) string {
	var cutoff time.Time
	if hoursBack > 0 {
		cutoff = time.Now().Add(-time.Duration(hoursBack) * time.Hour)
	}
	var b strings.Builder
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			fmt.Fprintf(&b, "%s|missing\n", f)
			continue
		}
		if !cutoff.IsZero() && info.ModTime().Before(cutoff) {
			continue
		}
		fmt.Fprintf(&b, "%s|%d|%d\n", f, info.ModTime().UnixNano(), info.Size())
	}
	return b.String()
}
