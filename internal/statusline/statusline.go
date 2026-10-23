// Package statusline captures the plan rate limits Claude Code passes to its
// status line command. Claude Code reads them from the
// anthropic-ratelimit-unified-* headers on its own model responses, so they
// exist for setup-token sessions that can't call the OAuth usage endpoint.
// Claude Code keeps them in memory only: running as the status line command is
// the one way for another process to receive them.
package statusline

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/sammcj/ccu/internal/oauth"
)

const (
	fileName = "rate-limits.json"
	// maxInput bounds stdin; status line payloads are a few KiB
	maxInput = 1 << 20
	// maxFile bounds the saved file: up to maxSessions sessions of about 300
	// bytes each
	maxFile     = 64 << 10
	maxSessions = 100
	// staleTemp is how old a temp file must be before it counts as left behind
	// by a run that Claude Code cancelled mid-write
	staleTemp = time.Minute

	fiveHours = 5 * time.Hour
	week      = 7 * 24 * time.Hour

	usageText = "usage: ccu statusline [-- command args...]\nSaves Claude Code's plan rate limits for the ccu dashboard. With a command, pipes the status line JSON to it and prints its output."
)

type window struct {
	UsedPercentage *float64 `json:"used_percentage"`
	ResetsAt       *float64 `json:"resets_at"` // Unix seconds
}

func (w *window) valid() bool {
	return w != nil && w.UsedPercentage != nil && w.ResetsAt != nil
}

func (w *window) equal(o *window) bool {
	if !w.valid() || !o.valid() {
		return w.valid() == o.valid()
	}
	return *w.UsedPercentage == *o.UsedPercentage && *w.ResetsAt == *o.ResetsAt
}

type rateLimits struct {
	FiveHour *window `json:"five_hour,omitempty"`
	SevenDay *window `json:"seven_day,omitempty"`
}

func (r *rateLimits) valid() bool {
	return r != nil && (r.FiveHour.valid() || r.SevenDay.valid())
}

func (r *rateLimits) equal(o *rateLimits) bool {
	return r.FiveHour.equal(o.FiveHour) && r.SevenDay.equal(o.SevenDay)
}

// session is the latest rate limits one Claude Code session sent
type session struct {
	APIDurationMS float64 `json:"api_duration_ms"`
	// CalledAt is when CCU saw APIDurationMS grow or RateLimits change, meaning
	// RateLimits had just arrived from the API. Zero until then: a session seen
	// for the first time may be replaying numbers of any age.
	CalledAt time.Time `json:"called_at,omitzero"`
	// ChangedAt is when the entry last changed, for pruning
	ChangedAt  time.Time  `json:"changed_at"`
	RateLimits rateLimits `json:"rate_limits"`
}

// snapshot is the saved file. Each Claude Code session keeps its own copy of
// the rate limits, and Load picks which to show.
type snapshot struct {
	Sessions map[string]session `json:"sessions"`
}

// DefaultPath sits alongside ccu.log in the user cache dir
func DefaultPath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ccu", fileName), nil
}

type statusInput struct {
	SessionID string `json:"session_id"`
	Cost      struct {
		TotalAPIDurationMS float64 `json:"total_api_duration_ms"`
	} `json:"cost"`
	RateLimits *rateLimits `json:"rate_limits"`
}

func parsePayload(payload []byte) (*statusInput, error) {
	var in statusInput
	if err := json.Unmarshal(payload, &in); err != nil {
		return nil, fmt.Errorf("parse status line payload: %w", err)
	}
	return &in, nil
}

// latest keeps a session's previous copy of a window only when the new payload
// lacks it: Claude Code omits a window once it resets
func latest(prev, in *window) *window {
	if in.valid() {
		return in
	}
	return prev
}

// record stores the payload's rate limits under its session and reports
// whether anything changed.
//
// Claude Code updates rate_limits from the headers of every API response, and
// total_api_duration_ms grows with every API call, so either changing dates
// the numbers. A process's counter is recorded before its first response too
// (when it sends no rate limits yet), including a lower one from a process
// resuming a session, so that its first numbers are dated.
func (s *snapshot) record(in *statusInput, now time.Time) bool {
	ms := in.Cost.TotalAPIDurationMS
	prev, seen := s.Sessions[in.SessionID]
	if seen && !in.RateLimits.valid() {
		if ms >= prev.APIDurationMS {
			// Leave the counter for the first response to grow
			return false
		}
		// A new process resuming the session. Until its first response the
		// numbers and their date are still the old process's.
		prev.APIDurationMS, prev.ChangedAt = ms, now
		s.Sessions[in.SessionID] = prev
		return true
	}

	next := session{APIDurationMS: ms, RateLimits: prev.RateLimits}
	if in.RateLimits != nil {
		next.RateLimits.FiveHour = latest(prev.RateLimits.FiveHour, in.RateLimits.FiveHour)
		next.RateLimits.SevenDay = latest(prev.RateLimits.SevenDay, in.RateLimits.SevenDay)
	}
	changed := !next.RateLimits.equal(&prev.RateLimits)
	switch {
	case !seen:
		// A first sighting: undated
	case ms > prev.APIDurationMS || changed:
		next.CalledAt = now
	case ms == prev.APIDurationMS:
		return false
	default:
		// A new process whose first payload was missed, with the same numbers:
		// undated
	}

	next.ChangedAt = now
	if s.Sessions == nil {
		s.Sessions = map[string]session{}
	}
	s.Sessions[in.SessionID] = next
	return true
}

// prune drops sessions unchanged for a week, since any numbers they hold are
// for windows that have since reset, and the oldest beyond maxSessions. A
// dropped session that is still open comes back undated.
func (s *snapshot) prune(now time.Time) {
	ids := slices.SortedFunc(maps.Keys(s.Sessions), func(a, b string) int {
		return s.Sessions[b].ChangedAt.Compare(s.Sessions[a].ChangedAt)
	})
	for i, id := range ids {
		if i >= maxSessions || now.Sub(s.Sessions[id].ChangedAt) > week {
			delete(s.Sessions, id)
		}
	}
}

// pick chooses which session's copy of a window to show: the one known to be
// most recent, whatever its usage, since Anthropic can reset a limit early.
// A copy is at least as recent as its session's last response, and as its
// window's start, since numbers for a window only exist once it has begun. So
// a later window beats undated copies of an earlier one, and a dated copy
// beats undated ones in its window. Among equals the highest usage wins.
func (s *snapshot) pick(slot func(rateLimits) *window, period time.Duration) (*window, session) {
	type candidate struct {
		id       string
		sess     session
		w        *window
		recentAs time.Time
	}
	var candidates []candidate
	for id, sess := range s.Sessions {
		w := slot(sess.RateLimits)
		if !w.valid() {
			continue
		}
		start := time.Unix(int64(*w.ResetsAt), 0).Add(-period)
		candidates = append(candidates, candidate{id, sess, w, latestTime(start, sess.CalledAt)})
	}
	if len(candidates) == 0 {
		return nil, session{}
	}
	best := slices.MaxFunc(candidates, func(a, b candidate) int {
		return cmp.Or(
			a.recentAs.Compare(b.recentAs),
			cmp.Compare(*a.w.UsedPercentage, *b.w.UsedPercentage),
			// Map order is random; keep the choice stable
			strings.Compare(b.id, a.id),
		)
	})
	return best.w, best.sess
}

func latestTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// Capture records the rate limits from a status line payload in the saved
// file
func Capture(payload []byte, path string, now time.Time) error {
	in, err := parsePayload(payload)
	if err != nil {
		return err
	}
	if in.SessionID == "" {
		// Claude Code always sends one. Without it there's no telling whose
		// numbers these are.
		return nil
	}
	if !in.RateLimits.valid() {
		// Without plan limits to save (before a first response, or an API key
		// session), there's no reason to create the file
		if _, err := os.Stat(path); err != nil {
			return nil
		}
	}

	// A missing or unreadable file starts fresh, as does one saved before
	// sessions were tracked
	read := func() snapshot {
		if s, err := readSnapshot(path); err == nil {
			return *s
		}
		return snapshot{}
	}
	now = now.UTC()
	// Most runs are idle replays that change nothing, so check before locking
	if s := read(); !s.record(in, now) {
		return nil
	}

	unlock, err := lock(path)
	if err != nil {
		return err
	}
	defer unlock()
	s := read()
	if !s.record(in, now) {
		return nil
	}
	s.prune(now)

	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return writeAtomic(path, data, now)
}

// lock serialises Capture's read-modify-write across concurrent status line
// runs, so one session's update can't overwrite another's. The lock file's
// name must not match removeStaleTemps' pattern. Closing the file, or the
// process dying, releases the lock. On a filesystem without flock, Capture
// runs unlocked.
func lock(path string) (unlock func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(strings.TrimSuffix(path, ".json")+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil && !errors.Is(err, syscall.ENOTSUP) && !errors.Is(err, syscall.EOPNOTSUPP) && !errors.Is(err, syscall.ENOSYS) {
		_ = f.Close()
		return nil, err
	}
	return func() { _ = f.Close() }, nil
}

// writeAtomic renames a temp file over path, in a directory lock created, so
// concurrent Claude Code sessions and a reading ccu never see a partial file.
func writeAtomic(path string, data []byte, now time.Time) error {
	dir := filepath.Dir(path)
	removeStaleTemps(dir, now)

	tmp, err := os.CreateTemp(dir, fileName+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// removeStaleTemps clears temp files from runs Claude Code killed before the
// rename. Young ones may belong to a concurrent run and are left alone.
func removeStaleTemps(dir string, now time.Time) {
	matches, _ := filepath.Glob(filepath.Join(dir, fileName+".*"))
	for _, m := range matches {
		if info, err := os.Stat(m); err == nil && now.Sub(info.ModTime()) > staleTemp {
			_ = os.Remove(m)
		}
	}
}

func readSnapshot(path string) (*snapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxFile+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxFile {
		return nil, fmt.Errorf("%s is larger than %d bytes", path, maxFile)
	}
	var s snapshot
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &s, nil
}

// ErrNoUsage means the file holds no five-hour window yet: it was saved before
// sessions were tracked, or only by sessions before their first response or
// without plan limits
var ErrNoUsage = errors.New("no five-hour window recorded")

// Load returns the saved rate limits as UsageData so the dashboard renders
// them like OAuth data. FetchedAt is when a response last confirmed the older
// of the chosen windows, or zero when either is undated. A window that reset since is reported as 0% with
// its next reset: any Claude Code activity since then would have written newer
// numbers. Captures can be days old, unlike OAuth data, so this can't be left
// to UsageData.EffectiveFiveHour.
func Load(path string, now time.Time) (*oauth.UsageData, error) {
	s, err := readSnapshot(path)
	if err != nil {
		return nil, err
	}
	five, fiveFrom := s.pick(func(r rateLimits) *window { return r.FiveHour }, fiveHours)
	seven, sevenFrom := s.pick(func(r rateLimits) *window { return r.SevenDay }, week)
	// The dashboard has no meaningful rendering without a five-hour window
	if !five.valid() {
		return nil, fmt.Errorf("%s: %w", path, ErrNoUsage)
	}

	usage := &oauth.UsageData{FetchedAt: fiveFrom.CalledAt, Source: oauth.SourceStatusline}
	usage.FiveHour.Utilisation, usage.FiveHour.ResetsAt = rollForward(five, fiveHours, now)
	if seven.valid() {
		usage.SevenDay.Utilisation, usage.SevenDay.ResetsAt = rollForward(seven, week, now)
		if sevenFrom.CalledAt.Before(usage.FetchedAt) {
			usage.FetchedAt = sevenFrom.CalledAt
		}
	}
	return usage, nil
}

func rollForward(w *window, period time.Duration, now time.Time) (float64, string) {
	pct, resetsAt := *w.UsedPercentage, time.Unix(int64(*w.ResetsAt), 0).UTC()
	for !resetsAt.After(now) {
		pct, resetsAt = 0, resetsAt.Add(period)
	}
	return pct, resetsAt.Format(time.RFC3339)
}

// Summary is the status line printed when no command is wrapped
func Summary(payload []byte) string {
	in, err := parsePayload(payload)
	if err != nil || !in.RateLimits.valid() {
		return ""
	}
	var parts []string
	if w := in.RateLimits.FiveHour; w.valid() {
		parts = append(parts, fmt.Sprintf("5h %.0f%%", *w.UsedPercentage))
	}
	if w := in.RateLimits.SevenDay; w.valid() {
		parts = append(parts, fmt.Sprintf("7d %.0f%%", *w.UsedPercentage))
	}
	return strings.Join(parts, " | ")
}

// Run implements `ccu statusline [-- command args...]`. With a command, the
// payload is piped to it and its output passed through, so an existing status
// line keeps working. Capture failures go to stderr and never block the
// status line. An empty path skips the capture.
func Run(args []string, path string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Fprintln(stdout, usageText)
		return 0
	}

	payload, err := io.ReadAll(io.LimitReader(stdin, maxInput))
	if err != nil {
		fmt.Fprintf(stderr, "ccu statusline: read stdin: %v\n", err)
		return 1
	}
	if path != "" {
		if err := Capture(payload, path, time.Now()); err != nil {
			fmt.Fprintf(stderr, "ccu statusline: %v\n", err)
		}
	}

	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintln(stdout, Summary(payload))
		return 0
	}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(stderr, "ccu statusline: %v\n", err)
		return 1
	}
	return 0
}
