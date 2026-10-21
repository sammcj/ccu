package statusline

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sammcj/ccu/internal/oauth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Reset times used throughout: five_hour 2026-10-21 11:00 UTC, seven_day 2026-10-25 12:00 UTC
const (
	fiveReset  = 1792580400
	sevenReset = 1792929600
)

var captured = time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)

// payload mirrors the shape Claude Code sends, with unrelated fields CCU ignores
var payload = statusJSON(23.5, 41.2)

func statusJSON(five, seven float64) string {
	return fmt.Sprintf(`{
	"model": {"display_name": "Opus"},
	"rate_limits": {
		"five_hour": {"used_percentage": %v, "resets_at": %d},
		"seven_day": {"used_percentage": %v, "resets_at": %d}
	}
}`, five, fiveReset, seven, sevenReset)
}

func capture(t *testing.T, path, p string, at time.Time) {
	t.Helper()
	require.NoError(t, Capture([]byte(p), path, at))
}

func load(t *testing.T, path string, now time.Time) *oauth.UsageData {
	t.Helper()
	usage, err := Load(path, now)
	require.NoError(t, err)
	return usage
}

func TestCaptureAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", fileName)
	capture(t, path, payload, captured)

	usage := load(t, path, captured.Add(time.Minute))
	assert.InDelta(t, 23.5, usage.FiveHour.Utilisation, 0.001)
	assert.Equal(t, "2026-10-21T11:00:00Z", usage.FiveHour.ResetsAt)
	assert.InDelta(t, 41.2, usage.SevenDay.Utilisation, 0.001)
	assert.Equal(t, "2026-10-25T12:00:00Z", usage.SevenDay.ResetsAt)
	assert.Equal(t, captured, usage.FetchedAt)
	assert.Equal(t, oauth.SourceStatusline, usage.Source)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestCaptureMerge(t *testing.T) {
	sevenOnly := fmt.Sprintf(`{"rate_limits":{"seven_day":{"used_percentage":45,"resets_at":%d}}}`, sevenReset)

	tests := []struct {
		name        string
		second      string
		wantFive    float64
		wantSeven   float64
		wantUpdated time.Time
	}{
		{
			name:     "payloads without limits leave the file alone",
			second:   `{"model": {"display_name": "Opus"}}`,
			wantFive: 23.5, wantSeven: 41.2, wantUpdated: captured,
		},
		{
			name:     "incomplete window is ignored",
			second:   `{"rate_limits": {"five_hour": {"used_percentage": 10}}}`,
			wantFive: 23.5, wantSeven: 41.2, wantUpdated: captured,
		},
		{
			// Claude Code drops five_hour once it resets, so this is routine
			name:     "absent window keeps the previous copy",
			second:   sevenOnly,
			wantFive: 23.5, wantSeven: 45, wantUpdated: captured.Add(time.Hour),
		},
		{
			name:     "identical numbers keep the update time",
			second:   payload,
			wantFive: 23.5, wantSeven: 41.2, wantUpdated: captured,
		},
		{
			name:     "idle session replaying lower usage is ignored",
			second:   statusJSON(10, 30),
			wantFive: 23.5, wantSeven: 41.2, wantUpdated: captured,
		},
		{
			name:     "higher usage in the same window is taken",
			second:   statusJSON(30, 41.2),
			wantFive: 30, wantSeven: 41.2, wantUpdated: captured.Add(time.Hour),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), fileName)
			capture(t, path, payload, captured)
			capture(t, path, tt.second, captured.Add(time.Hour))

			usage := load(t, path, captured)
			assert.InDelta(t, tt.wantFive, usage.FiveHour.Utilisation, 0.001)
			assert.InDelta(t, tt.wantSeven, usage.SevenDay.Utilisation, 0.001)
			assert.Equal(t, tt.wantUpdated, usage.FetchedAt)
		})
	}
}

func TestNewerWindow(t *testing.T) {
	w := func(pct, resets float64) *window { return &window{UsedPercentage: &pct, ResetsAt: &resets} }

	assert.Equal(t, 5.0, *newer(w(80, fiveReset), w(5, fiveReset+5*3600)).UsedPercentage, "a later window replaces an earlier one")
	assert.Equal(t, 80.0, *newer(w(80, fiveReset+5*3600), w(5, fiveReset)).UsedPercentage, "an earlier window never replaces a later one")
	assert.Equal(t, 30.0, *newer(w(20, fiveReset), w(30, fiveReset+60)).UsedPercentage, "reset jitter counts as the same window")
}

func TestCaptureRejectsInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), fileName)
	assert.Error(t, Capture([]byte("not json"), path, captured))
	assert.NoFileExists(t, path)
}

func TestCaptureRemovesStaleTemps(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, fileName+".111")
	fresh := filepath.Join(dir, fileName+".222")
	require.NoError(t, os.WriteFile(stale, []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(fresh, []byte("x"), 0o600))
	require.NoError(t, os.Chtimes(stale, captured.Add(-time.Hour), captured.Add(-time.Hour)))
	require.NoError(t, os.Chtimes(fresh, captured, captured))

	capture(t, filepath.Join(dir, fileName), payload, captured)

	assert.NoFileExists(t, stale, "left behind by a cancelled run")
	assert.FileExists(t, fresh, "may belong to a concurrent run")
}

func TestLoadRollsWindowsForward(t *testing.T) {
	path := filepath.Join(t.TempDir(), fileName)
	capture(t, path, payload, captured)

	// Two days on, the captured five-hour window (reset 11:00) is long over
	usage := load(t, path, captured.Add(48*time.Hour))
	assert.Zero(t, usage.FiveHour.Utilisation)
	assert.Equal(t, "2026-10-23T13:00:00Z", usage.FiveHour.ResetsAt, "next 5h step after now")
	assert.InDelta(t, 41.2, usage.SevenDay.Utilisation, 0.001, "week hasn't reset yet")

	// Nine days on, the captured week (reset 25 Oct 12:00) has ended too
	usage = load(t, path, captured.Add(9*24*time.Hour))
	assert.Zero(t, usage.SevenDay.Utilisation)
	assert.Equal(t, "2026-11-01T12:00:00Z", usage.SevenDay.ResetsAt)
}

func TestLoadErrors(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
		return p
	}

	_, err := Load(filepath.Join(dir, "missing.json"), captured)
	assert.ErrorIs(t, err, os.ErrNotExist)

	_, err = Load(write("seven-only.json", fmt.Sprintf(`{"rate_limits":{"seven_day":{"used_percentage":1,"resets_at":%d}}}`, sevenReset)), captured)
	assert.ErrorContains(t, err, "no five-hour window")

	_, err = Load(write("big.json", strings.Repeat(" ", maxFile+1)), captured)
	assert.ErrorContains(t, err, "larger than")

	_, err = Load(write("corrupt.json", "{"), captured)
	assert.ErrorContains(t, err, "parse")
}

func TestSummary(t *testing.T) {
	assert.Equal(t, "5h 24% | 7d 41%", Summary([]byte(payload)))
	assert.Equal(t, "7d 41%", Summary([]byte(`{"rate_limits":{"seven_day":{"used_percentage":41.2,"resets_at":1}}}`)))
	assert.Empty(t, Summary([]byte(`{"model":{}}`)), "API key sessions have no rate limits")
}

func TestRun(t *testing.T) {
	run := func(args []string, path, stdin string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		code := Run(args, path, strings.NewReader(stdin), &stdout, &stderr)
		return code, stdout.String(), stderr.String()
	}

	t.Run("prints summary and captures", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), fileName)
		code, out, errOut := run(nil, path, payload)
		assert.Equal(t, 0, code)
		assert.Equal(t, "5h 24% | 7d 41%\n", out)
		assert.Empty(t, errOut)
		assert.FileExists(t, path)
	})

	t.Run("wrapped command receives the payload unchanged", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), fileName)
		code, out, _ := run([]string{"--", "cat"}, path, payload)
		assert.Equal(t, 0, code)
		assert.Equal(t, payload, out)
		assert.FileExists(t, path)
	})

	t.Run("wrapped command exit code is passed through", func(t *testing.T) {
		code, _, _ := run([]string{"sh", "-c", "exit 3"}, filepath.Join(t.TempDir(), fileName), payload)
		assert.Equal(t, 3, code)
	})

	t.Run("capture failure still renders the status line", func(t *testing.T) {
		code, out, errOut := run([]string{"cat"}, filepath.Join(t.TempDir(), fileName), "not json")
		assert.Equal(t, 0, code)
		assert.Equal(t, "not json", out)
		assert.Contains(t, errOut, "parse status line payload")
	})

	t.Run("missing command is reported", func(t *testing.T) {
		code, _, errOut := run([]string{"/nonexistent/ccu-test-cmd"}, filepath.Join(t.TempDir(), fileName), payload)
		assert.Equal(t, 1, code)
		assert.Contains(t, errOut, "ccu statusline:")
	})

	t.Run("empty path skips capture without writing to the cwd", func(t *testing.T) {
		t.Chdir(t.TempDir())
		code, out, errOut := run(nil, "", payload)
		assert.Equal(t, 0, code)
		assert.Equal(t, "5h 24% | 7d 41%\n", out)
		assert.Empty(t, errOut)
		entries, err := os.ReadDir(".")
		require.NoError(t, err)
		assert.Empty(t, entries)
	})

	t.Run("help prints usage", func(t *testing.T) {
		code, out, _ := run([]string{"--help"}, "", "")
		assert.Equal(t, 0, code)
		assert.Contains(t, out, "usage: ccu statusline")
	})
}
