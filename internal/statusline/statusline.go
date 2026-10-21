// Package statusline captures the plan rate limits Claude Code passes to its
// status line command. Claude Code reads them from the
// anthropic-ratelimit-unified-* headers on its own model responses, so they
// exist for setup-token sessions that can't call the OAuth usage endpoint.
// Claude Code keeps them in memory only: running as the status line command is
// the one way for another process to receive them.
package statusline

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sammcj/ccu/internal/oauth"
)

const (
	fileName = "rate-limits.json"
	// maxInput bounds stdin; status line payloads are a few KiB
	maxInput = 1 << 20
	// maxFile bounds the saved file, which holds two windows
	maxFile = 64 << 10
	// staleTemp is how old a temp file must be before it counts as left behind
	// by a run that Claude Code cancelled mid-write
	staleTemp = time.Minute

	fiveHours = 5 * time.Hour
	week      = 7 * 24 * time.Hour
	// sameWindowTolerance separates jitter in one window's resets_at from a new
	// window, whose reset is at least one full window length later
	sameWindowTolerance = 3600

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

// snapshot is the saved file. UpdatedAt is when the numbers last changed, not
// when the status line last ran: an idle session re-sends the same numbers on
// every refreshInterval tick.
type snapshot struct {
	UpdatedAt  time.Time  `json:"updated_at"`
	RateLimits rateLimits `json:"rate_limits"`
}

// DefaultPath sits alongside ccu.log in the user cache dir
func DefaultPath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ccu", fileName), nil
}

func parsePayload(payload []byte) (*rateLimits, error) {
	var in struct {
		RateLimits *rateLimits `json:"rate_limits"`
	}
	if err := json.Unmarshal(payload, &in); err != nil {
		return nil, fmt.Errorf("parse status line payload: %w", err)
	}
	if in.RateLimits == nil || (!in.RateLimits.FiveHour.valid() && !in.RateLimits.SevenDay.valid()) {
		return nil, nil
	}
	return in.RateLimits, nil
}

// newer picks which copy of a window to keep. Each Claude Code session holds
// its own copy from its last response, so an idle session can replay older
// numbers, and Claude Code omits a window once it resets. Usage within a
// window only grows, and a later window replaces an earlier one.
func newer(prev, in *window) *window {
	switch {
	case !in.valid():
		return prev
	case !prev.valid():
		return in
	}
	diff := *in.ResetsAt - *prev.ResetsAt
	if diff > -sameWindowTolerance && diff < sameWindowTolerance {
		if *in.UsedPercentage < *prev.UsedPercentage {
			return prev
		}
		return in
	}
	if diff < 0 {
		return prev
	}
	return in
}

// Capture merges the rate limits from a status line payload into the saved
// file. Payloads without them (API key sessions, or before a session's first
// response) leave it untouched.
func Capture(payload []byte, path string, now time.Time) error {
	in, err := parsePayload(payload)
	if err != nil || in == nil {
		return err
	}

	// A missing or unreadable file starts fresh
	var prev snapshot
	if s, err := readSnapshot(path); err == nil {
		prev = *s
	}
	merged := rateLimits{
		FiveHour: newer(prev.RateLimits.FiveHour, in.FiveHour),
		SevenDay: newer(prev.RateLimits.SevenDay, in.SevenDay),
	}
	if merged.FiveHour.equal(prev.RateLimits.FiveHour) && merged.SevenDay.equal(prev.RateLimits.SevenDay) {
		return nil
	}

	data, err := json.Marshal(snapshot{UpdatedAt: now.UTC(), RateLimits: merged})
	if err != nil {
		return err
	}
	return writeAtomic(path, data, now)
}

// writeAtomic renames a temp file over path so concurrent Claude Code sessions
// and a reading ccu never see a partial file.
func writeAtomic(path string, data []byte, now time.Time) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
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

// Load returns the saved rate limits as UsageData so the dashboard renders
// them like OAuth data. A window that reset after the last update is reported
// as 0% with its next reset: any Claude Code activity since then would have
// written newer numbers. Captures can be days old, unlike OAuth data, so this
// can't be left to UsageData.EffectiveFiveHour.
func Load(path string, now time.Time) (*oauth.UsageData, error) {
	s, err := readSnapshot(path)
	if err != nil {
		return nil, err
	}
	five, seven := s.RateLimits.FiveHour, s.RateLimits.SevenDay
	// The dashboard has no meaningful rendering without a five-hour window
	if !five.valid() {
		return nil, fmt.Errorf("%s has no five-hour window", path)
	}

	usage := &oauth.UsageData{FetchedAt: s.UpdatedAt, Source: oauth.SourceStatusline}
	usage.FiveHour.Utilisation, usage.FiveHour.ResetsAt = rollForward(five, fiveHours, now)
	if seven.valid() {
		usage.SevenDay.Utilisation, usage.SevenDay.ResetsAt = rollForward(seven, week, now)
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
	limits, err := parsePayload(payload)
	if err != nil || limits == nil {
		return ""
	}
	var parts []string
	if limits.FiveHour.valid() {
		parts = append(parts, fmt.Sprintf("5h %.0f%%", *limits.FiveHour.UsedPercentage))
	}
	if limits.SevenDay.valid() {
		parts = append(parts, fmt.Sprintf("7d %.0f%%", *limits.SevenDay.UsedPercentage))
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
