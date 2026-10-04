package vpnmgr

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The version call sits on the launch path: the client asks the installed
// sing-box what it is before deciding whether to update it. The binary is one
// the client downloads, so a wedged or half-written one is a real state, and
// without the timeout it would hold the launch until the user gave up.

func TestLocalSingboxVersionReadsTheVersion(t *testing.T) {
	exe := fakeSingbox(t, "#!/bin/sh\necho 'sing-box version 1.10.1'\n")
	m := &Manager{singboxExe: exe}
	if got := m.localSingboxVersion(); got != "1.10.1" {
		t.Errorf("localSingboxVersion = %q, want %q", got, "1.10.1")
	}
}

func TestLocalSingboxVersionGivesUpOnAWedgedBinary(t *testing.T) {
	exe := fakeSingbox(t, "#!/bin/sh\nsleep 30\n")
	restore := shrinkSingboxTimeout(t, 200*time.Millisecond)
	defer restore()

	m := &Manager{singboxExe: exe}
	start := time.Now()
	got := m.localSingboxVersion()
	elapsed := time.Since(start)

	if got != "" {
		t.Errorf("localSingboxVersion = %q, want empty for a binary that never answered", got)
	}
	if elapsed > 5*time.Second {
		t.Errorf("took %v — the call was not bounded", elapsed)
	}
}

// A binary that is not one (a truncated download, a text file) must read as
// "no version" rather than as an error the caller has to handle.
func TestLocalSingboxVersionOfSomethingThatIsNotSingbox(t *testing.T) {
	exe := fakeSingbox(t, "#!/bin/sh\necho 'not a version at all'\n")
	m := &Manager{singboxExe: exe}
	if got := m.localSingboxVersion(); got != "" {
		t.Errorf("localSingboxVersion = %q, want empty", got)
	}
	if got := (&Manager{singboxExe: ""}).localSingboxVersion(); got != "" {
		t.Errorf("localSingboxVersion with no binary = %q, want empty", got)
	}
}

func fakeSingbox(t *testing.T, script string) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "sing-box")
	if err := os.WriteFile(exe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func shrinkSingboxTimeout(t *testing.T, d time.Duration) func() {
	t.Helper()
	previous := singboxCallTimeout
	singboxCallTimeout = d
	return func() { singboxCallTimeout = previous }
}
