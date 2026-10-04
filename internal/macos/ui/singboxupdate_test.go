//go:build darwin

package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestSingboxCheckDue(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	const jitter = 10 * time.Minute

	tests := []struct {
		name   string
		last   time.Time
		failed bool
		want   bool
	}{
		// No state file: a fresh install, which is also the one with no binary
		// at all — it must never wait for a window to open.
		{"never checked", time.Time{}, false, true},
		{"just checked", now.Add(-time.Minute), false, false},
		{"inside the interval", now.Add(-singboxCheckInterval + time.Minute), false, false},
		// The jitter is part of the wait, so the machine is not due the instant
		// the bare interval elapses.
		{"interval elapsed but not the jitter", now.Add(-singboxCheckInterval - time.Minute), false, false},
		{"interval and jitter elapsed", now.Add(-singboxCheckInterval - jitter), false, true},
		// A failed check retries on the shorter schedule.
		{"failed, still cooling down", now.Add(-singboxRetryInterval + time.Minute), true, false},
		{"failed, retry due", now.Add(-singboxRetryInterval), true, true},
		// A failure retries sooner than a success would.
		{"failed, would not be due after a success", now.Add(-singboxRetryInterval - time.Minute), true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := singboxCheckDue(tt.last, tt.failed, jitter, now); got != tt.want {
				t.Errorf("singboxCheckDue = %v, want %v", got, tt.want)
			}
		})
	}
}

// The offset must be stable for a machine (otherwise due-ness flaps around the
// boundary) and differ between machines (otherwise a fleet checks in lockstep).
func TestSingboxJitterFor(t *testing.T) {
	const window = 30 * time.Minute
	a1 := singboxJitterFor("mac-of-egor.local", window)
	a2 := singboxJitterFor("mac-of-egor.local", window)
	b := singboxJitterFor("mac-of-someone-else.local", window)

	if a1 != a2 {
		t.Errorf("same host gave %v then %v", a1, a2)
	}
	if a1 == b {
		t.Errorf("two hosts landed on the same offset %v", a1)
	}
	for _, d := range []time.Duration{a1, b} {
		if d < 0 || d >= window {
			t.Errorf("offset %v outside the window %v", d, window)
		}
	}
	if singboxJitterFor("any", 0) != 0 {
		t.Error("a zero window must produce no offset")
	}
}

func TestSingboxCheckStateRoundTrip(t *testing.T) {
	at := time.Unix(1785000000, 0)
	for _, ok := range []bool{true, false} {
		gotAt, failed, parsed := parseSingboxCheck(formatSingboxCheck(at, ok))
		if !parsed || !gotAt.Equal(at) || failed == ok {
			t.Errorf("round trip(ok=%v) = %v/%v/%v", ok, gotAt, failed, parsed)
		}
	}
	// Anything unreadable reads as "never checked", so a truncated file makes
	// the client check again rather than sit out the schedule forever.
	for _, bad := range []string{"", "   ", "notanumber ok", "1785000000"} {
		if _, _, parsed := parseSingboxCheck(bad); parsed {
			t.Errorf("parseSingboxCheck(%q) reported a usable record", bad)
		}
	}
}

// The check state lives in the user's run directory and outlives the binary it
// describes. After a reinstall that replaced the install directory, a "checked
// recently, all good" from before left the client with no sing-box and no
// intention of fetching one until the interval expired — six hours of "tool not
// found" every fifteen seconds.
func TestSingboxCheckDueIsNotTheOnlyGate(t *testing.T) {
	now := time.Date(2026, 8, 5, 23, 0, 0, 0, time.UTC)
	recent := now.Add(-time.Minute)
	if singboxCheckDue(recent, false, 0, now) {
		t.Fatal("precondition: a check a minute ago is not due on schedule")
	}
	// singboxUpdateTick must still run one, because the binary is gone. The gate
	// it uses is singboxMissing, tested below against a real directory.
}

func TestSingboxMissingLooksNextToTheBinary(t *testing.T) {
	notOnPath := func(string) (string, error) { return "", exec.ErrNotFound }
	onPath := func(string) (string, error) { return "/somewhere/sing-box", nil }

	dir := t.TempDir()
	if !singboxMissingIn(dir, notOnPath) {
		t.Error("an empty directory should count as missing")
	}
	if err := os.WriteFile(filepath.Join(dir, "sing-box"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if singboxMissingIn(dir, notOnPath) {
		t.Error("a sing-box in the install directory is not missing")
	}
	// One on PATH counts as well: a developer's own build must not be
	// downloaded over.
	if singboxMissingIn(t.TempDir(), onPath) {
		t.Error("a sing-box on PATH should count as present")
	}
}
