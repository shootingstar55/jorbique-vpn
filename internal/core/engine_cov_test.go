//go:build !windows

package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// reapSingbox waits for the process, drops the cached running state, and logs
// why it went: the exit status and the tail of what it printed.
func TestReapSingboxLogsTheExit(t *testing.T) {
	for _, tc := range []struct {
		name, script, wantMsg, wantLevel string
		withLog                          bool
	}{
		{"no logger", "exit 0", "", "", false},
		{"clean and silent", "exit 0", "sing-box process exited cleanly", "INFO", true},
		{"crash with output", "echo first; echo second >&2; exit 3",
			"sing-box process exited (exit status 3): first | second", "WARNING", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isRunningMu.Lock()
			isRunningCached, isRunningCachedAt = true, time.Now()
			epoch := isRunningEpoch
			isRunningMu.Unlock()

			out := &syncBuffer{}
			cmd := exec.Command("/bin/sh", "-c", tc.script)
			cmd.Stdout, cmd.Stderr = out, out
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			var got [][2]string
			var logFn func(string, string)
			if tc.withLog {
				logFn = func(msg, level string) { got = append(got, [2]string{msg, level}) }
			}
			reapSingbox(cmd, out, logFn)

			isRunningMu.Lock()
			invalidated := isRunningEpoch > epoch && isRunningCachedAt.IsZero()
			isRunningMu.Unlock()
			if !invalidated {
				t.Error("the exit did not invalidate the running cache")
			}
			if !tc.withLog {
				return
			}
			if len(got) != 1 || got[0] != [2]string{tc.wantMsg, tc.wantLevel} {
				t.Fatalf("logged %q, want [%q %q]", got, tc.wantMsg, tc.wantLevel)
			}
		})
	}
}

// What sing-box printed wins; with nothing printed, the tail of its own log
// file says why it failed.
func TestSingboxStartupError(t *testing.T) {
	const generic = "Sing-box failed to start or exited immediately"
	dataDir := t.TempDir()
	cfgPath := writeRuntimeConfig(t, dataDir) // log.output: logs/tun.log

	printed := &syncBuffer{}
	_, _ = printed.Write([]byte("  FATAL start inbound/tun: permission denied \n"))
	if got := singboxStartupError(printed, cfgPath, dataDir); got != "FATAL start inbound/tun: permission denied" {
		t.Errorf("printed output: %q", got)
	}

	if got := singboxStartupError(&syncBuffer{}, cfgPath, dataDir); got != generic {
		t.Errorf("no output, no log file: %q", got)
	}

	if err := os.MkdirAll(filepath.Join(dataDir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	logLine := "ERROR configure tun interface: TUN interface not ready\n"
	if err := os.WriteFile(filepath.Join(dataDir, "logs", "tun.log"), []byte(logLine), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := singboxStartupError(nil, cfgPath, dataDir); got != generic+": ERROR configure tun interface: TUN interface not ready" {
		t.Errorf("log tail: %q", got)
	}
}
