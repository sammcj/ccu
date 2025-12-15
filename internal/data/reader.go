package data

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sammcj/ccu/internal/models"
)

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

	for _, file := range files {
		// Check file modification time - skip old files entirely
		if hoursBack > 0 {
			info, err := os.Stat(file)
			if err == nil && info.ModTime().Before(cutoff) {
				// File hasn't been modified since cutoff, skip entirely
				continue
			}
		}

		entries, err := readJSONLFileWithFilter(file, cutoff, seen)
		if err != nil {
			// Silently skip files that can't be read (e.g., too large)
			// These are typically file-history snapshots that aren't needed
			continue
		}
		allEntries = append(allEntries, entries...)
	}

	// Sort by timestamp
	allEntries = SortEntriesByTime(allEntries)

	return allEntries, nil
}

// readJSONLFileWithFilter reads a JSONL file with time filtering and deduplication
// cutoff: entries before this time are skipped (zero value = no filter)
// seen: map for deduplication (nil = no deduplication)
func readJSONLFileWithFilter(filePath string, cutoff time.Time, seen map[string]bool) ([]models.UsageEntry, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("opening file: %w", err)
	}
	defer file.Close()

	var entries []models.UsageEntry
	scanner := bufio.NewScanner(file)

	// Increase buffer size for large lines (10MB to handle large JSONL files)
	const maxCapacity = 10 * 1024 * 1024 // 10MB
	buf := make([]byte, maxCapacity)
	scanner.Buffer(buf, maxCapacity)

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
			// Suppress warnings for cleaner output
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
		return nil, fmt.Errorf("scanning file: %w", err)
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

	// Read and process entries
	entries, err := ReadUsageEntries(files, hoursBack)
	if err != nil {
		return nil, fmt.Errorf("reading usage entries: %w", err)
	}

	return entries, nil
}
