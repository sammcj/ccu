package data

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// entryLine builds a valid assistant JSONL line for tests.
func entryLine(ts time.Time, msgID, reqID string, in, out int) string {
	return fmt.Sprintf(
		`{"type":"assistant","timestamp":%q,"requestId":%q,"message":{"id":%q,"model":"claude-sonnet-4-20250514","usage":{"input_tokens":%d,"output_tokens":%d}}}`,
		ts.UTC().Format(time.RFC3339), reqID, msgID, in, out)
}

func writeJSONL(t *testing.T, dir, name string, lines ...string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644))
	return path
}

// lowerMaxLineBytes shrinks the scanner line cap for the duration of a test so
// oversized-line behaviour can be exercised without writing 10 MiB files.
func lowerMaxLineBytes(t *testing.T, n int) {
	t.Helper()
	old := maxJSONLLineBytes
	maxJSONLLineBytes = n
	t.Cleanup(func() { maxJSONLLineBytes = old })
}

func TestReadJSONLFileWithFilter(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	dir := t.TempDir()

	t.Run("normal file", func(t *testing.T) {
		path := writeJSONL(t, dir, "normal.jsonl",
			entryLine(now.Add(-2*time.Hour), "msg_1", "req_1", 10, 5),
			`{"type":"user","timestamp":"2026-07-03T10:00:00Z"}`,
			entryLine(now.Add(-1*time.Hour), "msg_2", "req_2", 20, 10),
			`{"type":"assistant","timestamp":"2026-07-03T10:00:00Z","requestId":"req_z","message":{"id":"msg_z","usage":{"input_tokens":0,"output_tokens":0}}}`,
		)

		entries, err := readJSONLFileWithFilter(path, time.Time{}, nil, make([]byte, 1024), nil)
		require.NoError(t, err)
		require.Len(t, entries, 2)
		assert.Equal(t, "msg_1", entries[0].MessageID)
		assert.Equal(t, "msg_2", entries[1].MessageID)
	})

	t.Run("empty lines skipped", func(t *testing.T) {
		path := writeJSONL(t, dir, "empty_lines.jsonl",
			"",
			entryLine(now, "msg_1", "req_1", 10, 5),
			"",
			"",
			entryLine(now, "msg_2", "req_2", 20, 10),
			"",
		)

		entries, err := readJSONLFileWithFilter(path, time.Time{}, nil, make([]byte, 1024), nil)
		require.NoError(t, err)
		assert.Len(t, entries, 2)
	})

	t.Run("cutoff filtering", func(t *testing.T) {
		path := writeJSONL(t, dir, "cutoff.jsonl",
			entryLine(now.Add(-2*time.Hour), "msg_old", "req_old", 10, 5),
			entryLine(now.Add(-30*time.Minute), "msg_new", "req_new", 20, 10),
		)

		entries, err := readJSONLFileWithFilter(path, now.Add(-time.Hour), nil, make([]byte, 1024), nil)
		require.NoError(t, err)
		require.Len(t, entries, 1)
		assert.Equal(t, "msg_new", entries[0].MessageID)
	})

	t.Run("dedupe across two files sharing IDs", func(t *testing.T) {
		file1 := writeJSONL(t, dir, "dedupe1.jsonl",
			entryLine(now, "msg_a", "req_a", 10, 5),
			entryLine(now, "msg_b", "req_b", 20, 10),
		)
		file2 := writeJSONL(t, dir, "dedupe2.jsonl",
			entryLine(now, "msg_b", "req_b", 20, 10),
			entryLine(now, "msg_c", "req_c", 30, 15),
		)

		seen := make(map[string]bool)
		buf := make([]byte, 1024)
		entries1, err := readJSONLFileWithFilter(file1, time.Time{}, seen, buf, nil)
		require.NoError(t, err)
		entries2, err := readJSONLFileWithFilter(file2, time.Time{}, seen, buf, nil)
		require.NoError(t, err)

		assert.Len(t, entries1, 2)
		require.Len(t, entries2, 1)
		assert.Equal(t, "msg_c", entries2[0].MessageID)
	})

	t.Run("oversized line returns partial entries with error", func(t *testing.T) {
		lowerMaxLineBytes(t, 256)
		path := writeJSONL(t, dir, "oversized.jsonl",
			entryLine(now, "msg_1", "req_1", 10, 5),
			strings.Repeat("x", 1024),
			entryLine(now, "msg_2", "req_2", 20, 10),
		)

		stats := &readStats{}
		entries, err := readJSONLFileWithFilter(path, time.Time{}, nil, make([]byte, 64), stats)
		assert.Error(t, err)
		// Entries parsed before the oversized line survive; the scan stops at
		// the bad line so entries after it are unreachable.
		require.Len(t, entries, 1)
		assert.Equal(t, "msg_1", entries[0].MessageID)
	})
}

func TestLoadUsageData_Failures(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	t.Run("oversized line keeps partial entries from that file", func(t *testing.T) {
		resetLoadCache()
		lowerMaxLineBytes(t, 4096)
		dir := t.TempDir()
		writeJSONL(t, dir, "bad.jsonl",
			entryLine(now.Add(-5*time.Minute), "msg_partial", "req_partial", 10, 5),
			strings.Repeat("x", 70*1024), // exceeds the 64 KiB shared scan buffer
		)
		writeJSONL(t, dir, "good.jsonl",
			entryLine(now.Add(-4*time.Minute), "msg_good", "req_good", 20, 10),
		)

		entries, err := LoadUsageData(dir, 0)
		require.NoError(t, err)
		// bad.jsonl's valid entry before the oversized line survives alongside
		// good.jsonl's entry, sorted oldest first.
		require.Len(t, entries, 2)
		assert.Equal(t, "msg_partial", entries[0].MessageID)
		assert.Equal(t, "msg_good", entries[1].MessageID)
	})

	t.Run("unreadable file is retried once readable again", func(t *testing.T) {
		resetLoadCache()
		dir := t.TempDir()
		locked := writeJSONL(t, dir, "locked.jsonl",
			entryLine(now.Add(-5*time.Minute), "msg_locked", "req_locked", 10, 5),
		)
		writeJSONL(t, dir, "open.jsonl",
			entryLine(now.Add(-4*time.Minute), "msg_open", "req_open", 20, 10),
		)
		require.NoError(t, os.Chmod(locked, 0o000))
		t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })

		entries, err := LoadUsageData(dir, 0)
		require.NoError(t, err)
		require.Len(t, entries, 1)
		assert.Equal(t, "msg_open", entries[0].MessageID)

		// chmod changes neither mtime nor size, so the fingerprint is
		// unchanged; the failed file must still not be served from cache as
		// permanently empty once it becomes readable again.
		require.NoError(t, os.Chmod(locked, 0o644))
		entries, err = LoadUsageData(dir, 0)
		require.NoError(t, err)
		require.Len(t, entries, 2)
		assert.Equal(t, "msg_locked", entries[0].MessageID)
	})
}

func resetLoadCache() {
	globalLoadCache.mu.Lock()
	defer globalLoadCache.mu.Unlock()
	globalLoadCache.hoursBack = 0
	globalLoadCache.fingerprint = 0
	globalLoadCache.valid = false
	globalLoadCache.files = nil
	globalLoadCache.merged = nil
}

func TestLoadUsageData(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	t.Run("cache hit returns identical slice", func(t *testing.T) {
		resetLoadCache()
		dir := t.TempDir()
		writeJSONL(t, dir, "a.jsonl",
			entryLine(now.Add(-10*time.Minute), "msg_1", "req_1", 10, 5),
		)

		first, err := LoadUsageData(dir, 24)
		require.NoError(t, err)
		require.NotEmpty(t, first)

		second, err := LoadUsageData(dir, 24)
		require.NoError(t, err)
		require.Len(t, second, len(first))
		assert.True(t, &first[0] == &second[0], "expected pointer-identical backing array on cache hit")
	})

	t.Run("touching a file invalidates", func(t *testing.T) {
		resetLoadCache()
		dir := t.TempDir()
		path := writeJSONL(t, dir, "a.jsonl",
			entryLine(now.Add(-10*time.Minute), "msg_1", "req_1", 10, 5),
		)

		first, err := LoadUsageData(dir, 24)
		require.NoError(t, err)
		require.Len(t, first, 1)

		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
		require.NoError(t, err)
		_, err = f.WriteString(entryLine(now.Add(-5*time.Minute), "msg_2", "req_2", 20, 10) + "\n")
		require.NoError(t, err)
		require.NoError(t, f.Close())

		second, err := LoadUsageData(dir, 24)
		require.NoError(t, err)
		require.Len(t, second, 2)
		assert.Equal(t, "msg_2", second[1].MessageID)
	})

	t.Run("hoursBack change invalidates", func(t *testing.T) {
		resetLoadCache()
		dir := t.TempDir()
		writeJSONL(t, dir, "a.jsonl",
			entryLine(now.Add(-30*time.Hour), "msg_old", "req_old", 10, 5),
			entryLine(now.Add(-1*time.Hour), "msg_new", "req_new", 20, 10),
		)

		first, err := LoadUsageData(dir, 24)
		require.NoError(t, err)
		require.Len(t, first, 1)
		assert.Equal(t, "msg_new", first[0].MessageID)

		// Widening the window must reparse: the cached per-file entries were
		// parse-time filtered against the 24h cutoff.
		second, err := LoadUsageData(dir, 48)
		require.NoError(t, err)
		require.Len(t, second, 2)
		assert.Equal(t, "msg_old", second[0].MessageID)
	})

	t.Run("only changed file re-parsed", func(t *testing.T) {
		resetLoadCache()
		dir := t.TempDir()
		fixedTime := now.Add(-10 * time.Minute)
		pathA := writeJSONL(t, dir, "a.jsonl",
			entryLine(now.Add(-20*time.Minute), "msg_a1", "req_a1", 10, 5),
		)
		pathB := writeJSONL(t, dir, "b.jsonl",
			entryLine(now.Add(-15*time.Minute), "msg_b1", "req_b1", 20, 10),
		)
		require.NoError(t, os.Chtimes(pathA, fixedTime, fixedTime))
		require.NoError(t, os.Chtimes(pathB, fixedTime, fixedTime))

		first, err := LoadUsageData(dir, 24)
		require.NoError(t, err)
		require.Len(t, first, 2)

		// Rewrite b with different content but identical size and mtime: the
		// per-file cache must treat it as unchanged and keep the old entries.
		writeJSONL(t, dir, "b.jsonl",
			entryLine(now.Add(-15*time.Minute), "msg_b9", "req_b9", 20, 10),
		)
		require.NoError(t, os.Chtimes(pathB, fixedTime, fixedTime))

		// Grow a so the load can't take the fast path.
		f, err := os.OpenFile(pathA, os.O_APPEND|os.O_WRONLY, 0o644)
		require.NoError(t, err)
		_, err = f.WriteString(entryLine(now.Add(-5*time.Minute), "msg_a2", "req_a2", 30, 15) + "\n")
		require.NoError(t, err)
		require.NoError(t, f.Close())

		second, err := LoadUsageData(dir, 24)
		require.NoError(t, err)
		ids := make([]string, len(second))
		for i, e := range second {
			ids[i] = e.MessageID
		}
		assert.ElementsMatch(t, []string{"msg_a1", "msg_a2", "msg_b1"}, ids,
			"b.jsonl must come from cache (msg_b1), not a reparse (msg_b9)")
	})

	t.Run("deleted file evicted", func(t *testing.T) {
		resetLoadCache()
		dir := t.TempDir()
		writeJSONL(t, dir, "a.jsonl",
			entryLine(now.Add(-10*time.Minute), "msg_a", "req_a", 10, 5),
		)
		pathB := writeJSONL(t, dir, "b.jsonl",
			entryLine(now.Add(-5*time.Minute), "msg_b", "req_b", 20, 10),
		)

		first, err := LoadUsageData(dir, 24)
		require.NoError(t, err)
		require.Len(t, first, 2)

		require.NoError(t, os.Remove(pathB))

		second, err := LoadUsageData(dir, 24)
		require.NoError(t, err)
		require.Len(t, second, 1)
		assert.Equal(t, "msg_a", second[0].MessageID)
	})

	t.Run("cross-file dedupe after partial re-parse", func(t *testing.T) {
		resetLoadCache()
		dir := t.TempDir()
		writeJSONL(t, dir, "a.jsonl",
			entryLine(now.Add(-20*time.Minute), "msg_shared", "req_shared", 10, 5),
			entryLine(now.Add(-19*time.Minute), "msg_a1", "req_a1", 20, 10),
		)
		pathB := writeJSONL(t, dir, "b.jsonl",
			entryLine(now.Add(-20*time.Minute), "msg_shared", "req_shared", 10, 5),
			entryLine(now.Add(-18*time.Minute), "msg_b1", "req_b1", 30, 15),
		)

		first, err := LoadUsageData(dir, 24)
		require.NoError(t, err)
		require.Len(t, first, 3, "shared entry must appear once")

		// Grow b; a stays cached with its pre-dedupe entries.
		f, err := os.OpenFile(pathB, os.O_APPEND|os.O_WRONLY, 0o644)
		require.NoError(t, err)
		_, err = f.WriteString(entryLine(now.Add(-5*time.Minute), "msg_b2", "req_b2", 40, 20) + "\n")
		require.NoError(t, err)
		require.NoError(t, f.Close())

		second, err := LoadUsageData(dir, 24)
		require.NoError(t, err)
		require.Len(t, second, 4)
		sharedCount := 0
		for _, e := range second {
			if e.MessageID == "msg_shared" {
				sharedCount++
			}
		}
		assert.Equal(t, 1, sharedCount, "dedupe must hold across cached and re-parsed files")
	})

	t.Run("file older than cutoff excluded", func(t *testing.T) {
		resetLoadCache()
		dir := t.TempDir()
		writeJSONL(t, dir, "recent.jsonl",
			entryLine(now.Add(-10*time.Minute), "msg_recent", "req_recent", 10, 5),
		)
		old := writeJSONL(t, dir, "old.jsonl",
			entryLine(now.Add(-9*24*time.Hour), "msg_old", "req_old", 20, 10),
		)
		tenDaysAgo := now.Add(-10 * 24 * time.Hour)
		require.NoError(t, os.Chtimes(old, tenDaysAgo, tenDaysAgo))

		entries, err := LoadUsageData(dir, 24)
		require.NoError(t, err)
		require.Len(t, entries, 1)
		assert.Equal(t, "msg_recent", entries[0].MessageID)
	})

	t.Run("merged result sorted by timestamp", func(t *testing.T) {
		resetLoadCache()
		dir := t.TempDir()
		writeJSONL(t, dir, "a.jsonl",
			entryLine(now.Add(-5*time.Minute), "msg_2", "req_2", 10, 5),
		)
		writeJSONL(t, dir, "b.jsonl",
			entryLine(now.Add(-10*time.Minute), "msg_1", "req_1", 20, 10),
		)

		entries, err := LoadUsageData(dir, 24)
		require.NoError(t, err)
		require.Len(t, entries, 2)
		assert.Equal(t, "msg_1", entries[0].MessageID)
		assert.Equal(t, "msg_2", entries[1].MessageID)
	})
}
