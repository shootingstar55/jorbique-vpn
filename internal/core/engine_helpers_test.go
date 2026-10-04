package core

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"jorbiquevpn/internal/paths"
)

func TestLastLines(t *testing.T) {
	in := "first\n\nsecond\nthird\n\n"
	if got := lastLines(in, 2); got != "second | third" {
		t.Errorf("lastLines(2) = %q", got)
	}
	if got := lastLines(in, 10); got != "first | second | third" {
		t.Errorf("lastLines(all) = %q", got)
	}
	if got := lastLines("   ", 3); got != "" {
		t.Errorf("blank input = %q", got)
	}
}

func TestPIDFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if _, ok := readSingboxPID(dir); ok {
		t.Error("no PID file should read not-ok")
	}
	writeSingboxPIDFile(dir, 4321)
	pid, ok := readSingboxPID(dir)
	if !ok || pid != 4321 {
		t.Errorf("read pid = %d ok=%v", pid, ok)
	}
	if filepath.Base(singboxPidPath(dir)) != singboxPidFile {
		t.Errorf("pid path = %q", singboxPidPath(dir))
	}
	removeSingboxPIDFile(dir)
	if _, ok := readSingboxPID(dir); ok {
		t.Error("PID file not removed")
	}

	// Garbage / non-positive PID -> not-ok.
	_ = os.WriteFile(singboxPidPath(dir), []byte("notapid"), 0644)
	if _, ok := readSingboxPID(dir); ok {
		t.Error("garbage PID should be not-ok")
	}
	_ = os.WriteFile(singboxPidPath(dir), []byte("-5"), 0644)
	if _, ok := readSingboxPID(dir); ok {
		t.Error("negative PID should be not-ok")
	}
}

func TestSingboxLogTail(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "runtime.json")
	logsDir := filepath.Join(dir, "logs")
	_ = os.MkdirAll(logsDir, 0755)
	_ = os.WriteFile(cfgPath, []byte(`{"log":{"output":"logs/sb.log"}}`), 0644)
	_ = os.WriteFile(filepath.Join(logsDir, "sb.log"), []byte("a\nb\nFATAL boom\n"), 0644)

	tail := singboxLogTail(cfgPath, dir, 2)
	if !strings.Contains(tail, "FATAL boom") || !strings.Contains(tail, "b") {
		t.Errorf("tail = %q", tail)
	}

	// Missing config file -> "".
	if singboxLogTail(filepath.Join(dir, "nope.json"), dir, 2) != "" {
		t.Error("missing config should yield empty tail")
	}
	// Config without log.output -> "".
	_ = os.WriteFile(cfgPath, []byte(`{"log":{}}`), 0644)
	if singboxLogTail(cfgPath, dir, 2) != "" {
		t.Error("no log output should yield empty tail")
	}
}

func TestSyncBuffer(t *testing.T) {
	b := &syncBuffer{}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = b.Write([]byte("x"))
			_ = b.String()
		}()
	}
	wg.Wait()
	if len(b.String()) != 20 {
		t.Errorf("expected 20 bytes, got %d", len(b.String()))
	}
}

func TestIsRunningCacheLifecycle(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir()) // isRunningRaw reads paths.DataDir()
	invalidateIsRunningCache()

	// No PID file and no process -> false (stub platform).
	if IsRunning() {
		t.Error("IsRunning should be false with no PID file")
	}
	// Result is cached within the TTL.
	if v, ok := cachedIsRunning(); !ok || v {
		t.Errorf("expected cached false, got v=%v ok=%v", v, ok)
	}
	invalidateIsRunningCache()
	if _, ok := cachedIsRunning(); ok {
		t.Error("invalidate should drop the cached value")
	}
}

func TestIsRunningStalePIDFileRemoved(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	_ = os.MkdirAll(paths.DataDir(), 0755)
	writeSingboxPIDFile(paths.DataDir(), 999999) // a PID that is not our sing-box
	invalidateIsRunningCache()

	if IsRunning() {
		t.Error("a stale PID should resolve to not running")
	}
	if _, ok := readSingboxPID(paths.DataDir()); ok {
		t.Error("stale PID file should be removed")
	}
}

func TestWaitForPIDTimesOut(t *testing.T) {
	start := time.Now()
	if waitForPID(999999, 200*time.Millisecond) {
		t.Error("unknown PID should not be reported running")
	}
	if time.Since(start) < 150*time.Millisecond {
		t.Error("waitForPID returned too early")
	}
}

func TestWaitForTunInterfaceReturnsFalseWhenNotRunning(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	invalidateIsRunningCache()
	// IsRunning is false, so the wait loop bails out immediately rather than
	// blocking for the full connect timeout.
	if waitForTunInterface("") {
		t.Error("expected false when sing-box is not running")
	}
}

func TestStop(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	var levels []string
	logFn := func(msg, level string) { levels = append(levels, level) }

	// With a PID file present (taskkill is absent on the stub platform; the call
	// fails fast and the PID file is cleaned up regardless).
	_ = os.MkdirAll(paths.DataDir(), 0755)
	writeSingboxPIDFile(paths.DataDir(), 4242)
	Stop(logFn)
	if _, ok := readSingboxPID(paths.DataDir()); ok {
		t.Error("Stop should remove the PID file")
	}

	// Without a PID file (image-name kill branch).
	Stop(logFn)
	if len(levels) == 0 {
		t.Error("Stop should log")
	}
}
