package vpnmgr

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/macos/logger"
)

// covMgrLog is what the manager wrote to its app.log so far.
func covMgrLog(t *testing.T, m *Manager) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(m.Paths.LogDir, "app.log"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// covMgrRunning makes v look running: its pid file names a live child process.
func covMgrRunning(t *testing.T, m *Manager, v core.VPNItem) {
	t.Helper()
	if err := os.WriteFile(m.Paths.PIDFile(v.Name), []byte(strconv.Itoa(spawnUnkillable(t))+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// covMgrWaitFor polls cond until it holds; the scheduled checks run on their own goroutine.
func covMgrWaitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func covMgrWriteFile(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), mode); err != nil {
		t.Fatal(err)
	}
	// WriteFile's mode is filtered by the umask; the test needs the exact bits.
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

// covMgrDeadHost is a host:port nothing listens on any more.
func covMgrDeadHost(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	host := srv.Listener.Addr().String()
	srv.Close()
	return host
}

func TestCovMgrWarnMissingTools(t *testing.T) {
	bin := t.TempDir()
	covMgrWriteFile(t, filepath.Join(bin, "zz-covmgr-present"), 0o755)
	t.Setenv("PATH", bin)
	required := []string{singboxBinaryName, "zz-covmgr-absent", "zz-covmgr-present"}

	for _, tc := range []struct {
		name, singbox string
		want, notWant []string
	}{
		{"no sing-box", "", []string{"tool not found: sing-box", "tool not found: zz-covmgr-absent"}, []string{"zz-covmgr-present"}},
		{"sing-box found", "/opt/zz/sing-box", []string{"tool not found: zz-covmgr-absent"}, []string{"tool not found: sing-box", "zz-covmgr-present"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "app.log")
			warnMissingTools(required, tc.singbox, logger.New(path))
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range tc.want {
				if !strings.Contains(string(data), w) {
					t.Errorf("log lacks %q:\n%s", w, data)
				}
			}
			for _, w := range tc.notWant {
				if strings.Contains(string(data), w) {
					t.Errorf("log has %q:\n%s", w, data)
				}
			}
		})
	}
}

func TestCovMgrBundledSingboxExecutable(t *testing.T) {
	onPath := t.TempDir()
	covMgrWriteFile(t, filepath.Join(onPath, singboxBinaryName), 0o755)
	t.Setenv("PATH", onPath)

	for _, tc := range []struct {
		name string
		mode os.FileMode // 0: no sing-box next to the binary
		want func(binDir string) string
	}{
		{"executable next to the binary", 0o755, func(d string) string { return filepath.Join(d, singboxBinaryName) }},
		{"made executable when it is not", 0o644, func(d string) string { return filepath.Join(d, singboxBinaryName) }},
		{"falls back to PATH", 0, func(string) string { return filepath.Join(onPath, singboxBinaryName) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			binDir := t.TempDir()
			if tc.mode != 0 {
				covMgrWriteFile(t, filepath.Join(binDir, singboxBinaryName), tc.mode)
			}
			got := bundledSingboxExecutable(binDir, logger.New(filepath.Join(binDir, "app.log")))
			if want := tc.want(binDir); got != want {
				t.Fatalf("sing-box = %q, want %q", got, want)
			}
			if st, err := os.Stat(got); err != nil || st.Mode().Perm()&0o111 == 0 {
				t.Errorf("%s is not executable (%v)", got, err)
			}
		})
	}
}

// With nothing on the list and nothing running there is nothing to bring up,
// and the log says why.
func TestCovMgrAutostartTargetsExplainsAnEmptySet(t *testing.T) {
	for _, tc := range []struct {
		name, list, reason string
		write              bool
	}{
		{"no list", "", "file missing", false},
		{"empty list", "", "file empty", true},
		{"list names other VPNs", "zz-covmgr-other\n", "no VPN names from config matched list", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := failsToStart("covmgr-empty-set")
			m := testManager(t, v)
			if tc.write {
				if err := os.WriteFile(m.startedListURL(), []byte(tc.list), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if got := m.autostartTargets(nil); got != nil {
				t.Fatalf("targets = %v, want none", got)
			}
			if log := covMgrLog(t, m); !strings.Contains(log, "no saved targets; skipping") || !strings.Contains(log, tc.reason) {
				t.Errorf("log lacks %q:\n%s", tc.reason, log)
			}
		})
	}
}

func TestCovMgrAutostartTargetsFallsBackToWhatIsRunning(t *testing.T) {
	v := failsToStart("covmgr-running")
	m := testManager(t, v)
	covMgrRunning(t, m, v)

	got := m.autostartTargets(nil)
	if len(got) != 1 || got[0].Name != v.Name {
		t.Fatalf("targets = %v, want [%s]", got, v.Name)
	}
	if log := covMgrLog(t, m); !strings.Contains(log, "using currently running targets: "+v.Name) {
		t.Errorf("the fallback was not logged:\n%s", log)
	}
}

func TestCovMgrAutostartCheckWithoutTargets(t *testing.T) {
	v := failsToStart("covmgr-no-targets")
	m := testManager(t, v)

	if err := m.AutostartCheck(false, false); err != nil {
		t.Fatalf("AutostartCheck: %v", err)
	}
	if log := covMgrLog(t, m); !strings.Contains(log, "no saved targets") || strings.Contains(log, "will restart") {
		t.Errorf("log:\n%s", log)
	}
}

// A running target is left alone, and the run says so.
func TestCovMgrAutostartCheckLeavesHealthyTargetsAlone(t *testing.T) {
	v := failsToStart("covmgr-healthy")
	m := testManager(t, v)
	covMgrRunning(t, m, v)
	m.saveStartedList([]string{v.Name})

	if err := m.AutostartCheck(false, false); err != nil {
		t.Fatalf("AutostartCheck: %v", err)
	}
	if log := covMgrLog(t, m); !strings.Contains(log, "all targets running; no restart") || strings.Contains(log, "will restart") {
		t.Errorf("log:\n%s", log)
	}
}

func TestCovMgrLogAutostartResult(t *testing.T) {
	m := testManager(t)
	m.logAutostartResult([]core.VPNItem{{Name: "zz-a"}, {Name: "zz-b"}}, []string{"zz-c: boom"})
	log := covMgrLog(t, m)
	for _, want := range []string{"autostart-check: restarted zz-a, zz-b", "autostart-check: did not come back: zz-c: boom"} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
	if got := vpnItemNames(nil); got == nil || len(got) != 0 {
		t.Errorf("vpnItemNames(nil) = %#v, want an empty slice", got)
	}
}

// covMgrSchedManager has no UUID, so a scheduled check runs and returns at once.
func covMgrSchedManager(t *testing.T, fallback float64) *Manager {
	t.Helper()
	m := testManager(t)
	m.Cfg.Singbox.UUID = ""
	m.Cfg.Autostart = &core.AutostartSection{FallbackTimeoutSeconds: &fallback}
	return m
}

func covMgrStamped(m *Manager) bool {
	m.autoMu.Lock()
	defer m.autoMu.Unlock()
	return m.hasLastAutostartTime
}

// A check scheduled while asleep waits for the wake; if the wake never comes,
// the fallback runs it anyway and stamps it.
func TestCovMgrScheduleWhileAsleepFallsBack(t *testing.T) {
	m := covMgrSchedManager(t, 0.01)
	prev := make(chan struct{})
	m.isSystemSleeping, m.pendingAutoCancel = true, prev

	m.ScheduleAutostartCheck("Wake", time.Hour, false, false)
	select {
	case <-prev:
	default:
		t.Error("the check queued before was not cancelled")
	}
	covMgrWaitFor(t, "the fallback check", func() bool { return covMgrStamped(m) })

	m.autoMu.Lock()
	sleeping, pending := m.isSystemSleeping, m.hasPendingAutoCheck
	m.autoMu.Unlock()
	if sleeping || pending {
		t.Errorf("after the fallback: sleeping = %v, pending = %v", sleeping, pending)
	}
	if log := covMgrLog(t, m); !strings.Contains(log, "reason=fallback_wake_missed") {
		t.Errorf("log:\n%s", log)
	}
}

// A routine check runs after its delay and does not count as a full autostart.
func TestCovMgrScheduledCheckRuns(t *testing.T) {
	m := covMgrSchedManager(t, 20)
	m.ScheduleAutostartCheck("Heartbeat", 0, false, false)
	covMgrWaitFor(t, "the scheduled check", func() bool {
		return strings.Contains(covMgrLog(t, m), "no user UUID yet")
	})
	if !strings.Contains(covMgrLog(t, m), "reason=scheduled") {
		t.Error("the schedule was not logged")
	}
	if covMgrStamped(m) {
		t.Error("a routine check was stamped as a full autostart")
	}
}

func TestCovMgrAwaitMissedWakeStandsDown(t *testing.T) {
	t.Run("cancelled", func(t *testing.T) {
		m := covMgrSchedManager(t, 20)
		m.isSystemSleeping = true
		ch := make(chan struct{})
		close(ch)
		m.awaitMissedWake(ch, time.Hour)
		if covMgrStamped(m) || !m.isSystemSleeping {
			t.Error("a cancelled fallback still ran")
		}
	})
	t.Run("woke in time", func(t *testing.T) {
		m := covMgrSchedManager(t, 20)
		m.awaitMissedWake(make(chan struct{}), time.Millisecond)
		if covMgrStamped(m) {
			t.Error("the fallback ran although the wake was handled")
		}
	})
}

func TestCovMgrCheckUserUUIDRejectsAMalformedUUID(t *testing.T) {
	asked := answerUUIDChecks(t, nil)
	m := testManager(t)
	if err := m.checkUserUUID("not-a-uuid"); err == nil || !strings.Contains(err.Error(), "Invalid user UUID format") {
		t.Fatalf("checkUserUUID = %v", err)
	}
	if len(*asked) != 0 {
		t.Errorf("the server was asked about %q", *asked)
	}
}

func TestCovMgrTunnelStates(t *testing.T) {
	m := newFirstRunManager(t)
	cfg, err := core.ReadConfig(m.Paths)
	if err != nil {
		t.Fatal(err)
	}
	up := m.tunnelStates()
	if len(up) != len(cfg.VPNs) || len(up) == 0 {
		t.Fatalf("states = %v, want one per configured VPN %v", up, cfg.VPNs)
	}
	for _, v := range cfg.VPNs {
		if state, ok := up[v.Name]; !ok || state != m.IsRunning(v) {
			t.Errorf("%s: state = %v (present %v), want %v", v.Name, state, ok, m.IsRunning(v))
		}
	}

	// An unreadable config reports nothing rather than a guess.
	m.Paths.CfgFile = t.TempDir()
	if up := m.tunnelStates(); len(up) != 0 {
		t.Errorf("states from an unreadable config = %v", up)
	}
}

func TestCovMgrSetRouteRules(t *testing.T) {
	m := newFirstRunManager(t)
	if err := m.SetRouteRules([]string{" direct.example, more.example ", "zz-covmgr-proc", ""}); err != nil {
		t.Fatalf("SetRouteRules: %v", err)
	}
	if !reflect.DeepEqual(m.Cfg.DirectDomains, []string{"direct.example", "more.example"}) || !reflect.DeepEqual(m.Cfg.DirectBypassProcesses, []string{"zz-covmgr-proc"}) {
		t.Errorf("direct = %v, processes = %v", m.Cfg.DirectDomains, m.Cfg.DirectBypassProcesses)
	}

	m.Paths.CfgFile = filepath.Join(t.TempDir(), "missing.json")
	if err := m.SetRouteRules(nil); err == nil {
		t.Error("saving into a missing vpn_cfg.json reported success")
	}
}

func TestCovMgrLogPlistAndDaemonFields(t *testing.T) {
	m := testManager(t)
	m.logPlistFields([]byte("<key>ProgramArguments</key><array/><key>StandardOutPath</key><string>/x</string>"))
	m.logDaemonFields("\tstate = running\n\tenabled = 1\n")
	m.logDaemonFields("pid = 4242\nlast exit status = 0\n")

	log := covMgrLog(t, m)
	for _, want := range []string{
		"Plist fields: ProgramArguments ✓, StdOut ✓, StdErr ✗",
		"• enabled = 1", "• state = running", "pid not present (not running)",
		"• pid = 4242", "• last exit status = 0",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
	if n := strings.Count(log, "pid not present"); n != 1 {
		t.Errorf("pid reported missing %d times, want once", n)
	}
}
