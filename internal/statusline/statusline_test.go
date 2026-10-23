package statusline

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// minute is n minutes after captured
func minute(n int) time.Time { return captured.Add(time.Duration(n) * time.Minute) }

// payload mirrors the shape Claude Code sends, with unrelated fields CCU ignores
var payload = statusJSON(23.5, 41.2)

func statusJSON(five, seven float64) string {
	return fmt.Sprintf(`{
	"session_id": "s",
	"model": {"display_name": "Opus"},
	"rate_limits": {
		"five_hour": {"used_percentage": %v, "resets_at": %d},
		"seven_day": {"used_percentage": %v, "resets_at": %d}
	}
}`, five, fiveReset, seven, sevenReset)
}

// sessionJSON is a payload from session id after apiMS of API time, reporting
// the given seven-day usage
func sessionJSON(id string, apiMS, seven float64) string {
	return fmt.Sprintf(`{
	"session_id": %q,
	"cost": {"total_cost_usd": 1.5, "total_api_duration_ms": %v},
	"rate_limits": {
		"five_hour": {"used_percentage": 10, "resets_at": %d},
		"seven_day": {"used_percentage": %v, "resets_at": %d}
	}
}`, id, apiMS, fiveReset, seven, sevenReset)
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
	assert.Zero(t, usage.FetchedAt, "no response has confirmed numbers without a session")
	assert.Equal(t, oauth.SourceStatusline, usage.Source)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestCaptureMerge(t *testing.T) {
	sevenOnly := fmt.Sprintf(`{"session_id":"s","rate_limits":{"seven_day":{"used_percentage":45,"resets_at":%d}}}`, sevenReset)

	tests := []struct {
		name      string
		second    string
		wantFive  float64
		wantSeven float64
	}{
		{
			name:     "payloads without limits leave the file alone",
			second:   `{"model": {"display_name": "Opus"}}`,
			wantFive: 23.5, wantSeven: 41.2,
		},
		{
			name:     "incomplete window is ignored",
			second:   `{"rate_limits": {"five_hour": {"used_percentage": 10}}}`,
			wantFive: 23.5, wantSeven: 41.2,
		},
		{
			// Claude Code drops five_hour once it resets, so this is routine
			name:     "absent window keeps the previous copy",
			second:   sevenOnly,
			wantFive: 23.5, wantSeven: 45,
		},
		{
			name:     "identical numbers change nothing",
			second:   payload,
			wantFive: 23.5, wantSeven: 41.2,
		},
		{
			name:     "a session's new numbers replace its old ones",
			second:   statusJSON(10, 30),
			wantFive: 10, wantSeven: 30,
		},
		{
			name:     "another undated session replaying lower usage is ignored",
			second:   sessionJSON("idle", 1000, 30),
			wantFive: 23.5, wantSeven: 41.2,
		},
		{
			name:     "another undated session's higher usage is taken",
			second:   sessionJSON("other", 1000, 50),
			wantFive: 23.5, wantSeven: 50,
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
		})
	}
}

func TestPick(t *testing.T) {
	earlier, later := captured, captured.Add(time.Hour)
	w := func(pct, resets float64) *window { return &window{UsedPercentage: &pct, ResetsAt: &resets} }
	sess := func(called time.Time, seven *window) session {
		return session{CalledAt: called, RateLimits: rateLimits{SevenDay: seven}}
	}

	tests := []struct {
		name     string
		sessions map[string]session
		wantPct  float64
	}{
		{"a later window wins", map[string]session{
			"a": sess(later, w(80, sevenReset)), "b": sess(time.Time{}, w(5, sevenReset+7*86400)),
		}, 5},
		{"an earlier window wins with a response since the later one began", map[string]session{
			"a": sess(time.Time{}, w(80, sevenReset)), "b": sess(later, w(5, sevenReset-3*86400)),
		}, 5},
		{"the most recent response wins with lower usage", map[string]session{
			"a": sess(earlier, w(99, sevenReset)), "b": sess(later, w(70, sevenReset)),
		}, 70},
		{"dated numbers beat higher undated ones", map[string]session{
			"a": sess(time.Time{}, w(99, sevenReset)), "b": sess(earlier, w(70, sevenReset)),
		}, 70},
		{"undated numbers fall back to higher usage", map[string]session{
			"a": sess(time.Time{}, w(60, sevenReset)), "b": sess(time.Time{}, w(70, sevenReset)),
		}, 70},
		{"equally dated numbers fall back to higher usage", map[string]session{
			"a": sess(earlier, w(60, sevenReset)), "b": sess(earlier, w(70, sevenReset)),
		}, 70},
		{"sessions without the window are skipped", map[string]session{
			"a": sess(later, nil), "b": sess(time.Time{}, w(70, sevenReset)),
		}, 70},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := snapshot{Sessions: tt.sessions}
			got, _ := s.pick(func(r rateLimits) *window { return r.SevenDay }, week)
			assert.InDelta(t, tt.wantPct, *got.UsedPercentage, 0.001)
		})
	}
}

func TestRecord(t *testing.T) {
	var s snapshot
	record := func(p string, n int) bool {
		in, err := parsePayload([]byte(p))
		require.NoError(t, err)
		return s.record(in, minute(n))
	}
	calledAt := func(id string) time.Time { return s.Sessions[id].CalledAt }

	assert.True(t, record(sessionJSON("a", 1000, 50), 0))
	assert.Zero(t, calledAt("a"), "a first sighting may be replaying old numbers")
	assert.False(t, record(sessionJSON("a", 1000, 50), 1), "an idle replay changes nothing")

	assert.True(t, record(sessionJSON("a", 2500, 50), 2))
	assert.Equal(t, minute(2), calledAt("a"), "growing API time means the numbers just arrived")
	assert.False(t, record(sessionJSON("a", 2500, 50), 3))
	assert.Equal(t, minute(2), calledAt("a"))

	assert.True(t, record(sessionJSON("a", 300, 50), 4))
	assert.Zero(t, calledAt("a"), "a counter that went backwards is a new process")

	assert.True(t, record(`{"session_id": "a", "cost": {"total_api_duration_ms": 0}}`, 5), "a process resuming the session records its counter")
	assert.Equal(t, minute(5), s.Sessions["a"].ChangedAt, "so pruning keeps it")
	assert.True(t, record(sessionJSON("a", 200, 50), 5))
	assert.Equal(t, minute(5), calledAt("a"), "so its first response is dated")

	fresh := `{"session_id": "new", "cost": {"total_api_duration_ms": 0}}`
	assert.True(t, record(fresh, 5), "a session is recorded before its first response")
	assert.False(t, record(fresh, 6))
	assert.True(t, record(sessionJSON("new", 800, 50), 7))
	assert.Equal(t, minute(7), calledAt("new"), "so its first numbers are dated")

	sevenOnly := `{"session_id": "new", "cost": {"total_api_duration_ms": 900}, "rate_limits": {"seven_day": {"used_percentage": 60, "resets_at": 1792929600}}}`
	assert.True(t, record(sevenOnly, 8))
	assert.InDelta(t, 10, *s.Sessions["new"].RateLimits.FiveHour.UsedPercentage, 0.001, "an omitted window keeps the session's last copy")

	assert.True(t, record(sessionJSON("new", 900, 55), 9))
	assert.Equal(t, minute(9), calledAt("new"), "new numbers mean a response arrived, even before the counter grows")
}

func TestPrune(t *testing.T) {
	s := snapshot{Sessions: map[string]session{}}
	for i := range maxSessions + 5 {
		s.Sessions[fmt.Sprint(i)] = session{ChangedAt: captured.Add(time.Duration(i) * time.Minute)}
	}
	s.Sessions["old"] = session{ChangedAt: captured.Add(-week - time.Minute)}

	s.prune(captured)
	assert.Len(t, s.Sessions, maxSessions)
	assert.NotContains(t, s.Sessions, "old")
	assert.NotContains(t, s.Sessions, "4", "the oldest beyond the cap go")
	assert.Contains(t, s.Sessions, "5")
}

func TestCaptureLimitReset(t *testing.T) {
	path := filepath.Join(t.TempDir(), fileName)
	seven := func() float64 { return load(t, path, captured).SevenDay.Utilisation }
	fetched := func() time.Time { return load(t, path, captured).FetchedAt }

	// An undated copy from before the reset
	capture(t, path, statusJSON(10, 99), minute(0))

	capture(t, path, sessionJSON("live", 1000, 70), minute(1))
	assert.InDelta(t, 99, seven(), 0.001, "a first sighting can't lower usage")

	capture(t, path, sessionJSON("live", 4000, 70), minute(2))
	assert.InDelta(t, 70, seven(), 0.001, "the live session's next call shows the reset")
	assert.Equal(t, minute(2), fetched())

	capture(t, path, sessionJSON("idle", 9000, 98), minute(3))
	capture(t, path, sessionJSON("idle", 9000, 98), minute(4))
	capture(t, path, statusJSON(10, 99), minute(5))
	assert.InDelta(t, 70, seven(), 0.001, "idle and undated sessions can't restore the old value")

	capture(t, path, sessionJSON("live", 4000, 70), minute(6))
	assert.Equal(t, minute(2), fetched(), "replayed numbers keep the update time")
	capture(t, path, sessionJSON("live", 5000, 70), minute(7))
	assert.Equal(t, minute(7), fetched(), "numbers confirmed by a call count as an update")
}

func TestCaptureResumedSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), fileName)
	capture(t, path, sessionJSON("stale", 1000, 99), minute(0))
	capture(t, path, sessionJSON("stale", 2000, 99), minute(1))
	capture(t, path, sessionJSON("live", 1000, 70), minute(2))
	capture(t, path, sessionJSON("live", 3000, 70), minute(3))

	// A new process resumes "live", its counter restarting before a response
	capture(t, path, `{"session_id": "live", "cost": {"total_api_duration_ms": 0}}`, minute(4))
	assert.InDelta(t, 70, load(t, path, captured).SevenDay.Utilisation, 0.001, "the old process's date still stands")

	capture(t, path, sessionJSON("live", 500, 65), minute(5))
	usage := load(t, path, captured)
	assert.InDelta(t, 65, usage.SevenDay.Utilisation, 0.001)
	assert.Equal(t, minute(5), usage.FetchedAt, "its first response is dated")
}

func TestCaptureNewProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), fileName)
	capture(t, path, sessionJSON("old", 1000, 50), minute(0))
	capture(t, path, sessionJSON("old", 2000, 99), minute(1))

	// A new process sends status line input before its first response
	capture(t, path, `{"session_id": "new", "cost": {"total_api_duration_ms": 0}}`, minute(2))
	capture(t, path, sessionJSON("new", 700, 70), minute(3))
	assert.InDelta(t, 70, load(t, path, captured).SevenDay.Utilisation, 0.001)
}

func TestCaptureIgnoresPayloadsWithoutSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), fileName)
	capture(t, path, fmt.Sprintf(`{"rate_limits": {"five_hour": {"used_percentage": 5, "resets_at": %d}}}`, fiveReset), captured)
	assert.NoFileExists(t, path)
}

func TestCaptureWithoutLimitsCreatesNoFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), fileName)
	// e.g. an API key session, which never gets plan limits
	capture(t, path, `{"session_id": "a", "cost": {"total_api_duration_ms": 900}}`, captured)
	assert.NoFileExists(t, path)
}

func TestCaptureConcurrentSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), fileName)
	capture(t, path, payload, captured)

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			assert.NoError(t, Capture([]byte(sessionJSON(fmt.Sprint(i), 1000, 50)), path, captured))
		})
	}
	wg.Wait()

	s, err := readSnapshot(path)
	require.NoError(t, err)
	assert.Len(t, s.Sessions, 21, "no session's update is lost")
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

	_, err = Load(write("seven-only.json", fmt.Sprintf(`{"sessions":{"a":{"rate_limits":{"seven_day":{"used_percentage":1,"resets_at":%d}}}}}`, sevenReset)), captured)
	assert.ErrorIs(t, err, ErrNoUsage)

	_, err = Load(write("before-first-response.json", `{"sessions":{"a":{"api_duration_ms":0,"rate_limits":{}}}}`), captured)
	assert.ErrorIs(t, err, ErrNoUsage)

	_, err = Load(write("pre-sessions.json", payload), captured)
	assert.ErrorIs(t, err, ErrNoUsage, "files from before sessions were tracked are ignored")

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
