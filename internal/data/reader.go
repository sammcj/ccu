package data

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
func ReadUsageEntries(files []string, hoursBack int) ([]models.UsageEntry, error) {
	var allEntries []models.UsageEntry

	for _, file := range files {
		entries, err := readJSONLFile(file)
		if err != nil {
			// Silently skip files that can't be read (e.g., too large)
			// These are typically file-history snapshots that aren't needed
			continue
		}
		allEntries = append(allEntries, entries...)
	}

	// Deduplicate entries
	allEntries = DeduplicateEntries(allEntries)

	// Filter by time
	allEntries = FilterByTime(allEntries, hoursBack)

	// Sort by timestamp
	allEntries = SortEntriesByTime(allEntries)

	return allEntries, nil
}

// readJSONLFile reads a single JSONL file
func readJSONLFile(filePath string) ([]models.UsageEntry, error) {
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
			fmt.Fprintf(os.Stderr, "Warning: %s:%d: %v\n", filePath, lineNum, err)
			continue
		}

		// Skip nil entries (expected skips from parser)
		if entry == nil {
			continue
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
