package vpnmgr

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/macos/logger"
)

// A Manager with somewhere to put its files and nothing that talks to the
// system: no NetworkSetup, so the DNS reapply is a no-op, and a started list in
// a temp file rather than /Library.
func testManager(t *testing.T, vpns ...core.VPNItem) *Manager {
	t.Helper()
	dir := t.TempDir()
	listPath := filepath.Join(dir, "started.txt")
	return &Manager{
		Paths:  &core.Paths{RunDir: dir, LogDir: dir, StartedListFilename: "started.txt"},
		Logger: logger.New(filepath.Join(dir, "app.log")),
		Cfg:    &core.AppConfig{Singbox: core.SingboxSection{UUID: testUserUUID}, VPNs: vpns, StartedListPath: &listPath},
	}
}

// A VPN whose start fails immediately, before anything is spawned: a sing-box
// profile with no config file is refused by startOne on the first line of its
// branch.
//
// The names are deliberately ones no real profile uses. IsRunning looks for a
// pid file in every run directory the client knows — including the machine's
// own — and HealedRecently reads markers the same way, so a test named after a
// real VPN reads (and stops) the tunnel the developer is running.
func failsToStart(name string) core.VPNItem {
	return core.VPNItem{Name: "zz-test-" + name, Type: core.VPNSingbox}
}

// restartTargets returned nil no matter what happened, so a restart that brought
// back none of its tunnels reported exactly what a restart that brought back all
// of them did — and `jorbique-vpn restart <name>` exited 0 over a dead tunnel.
func TestRestartTargetsReportsWhatDidNotComeBack(t *testing.T) {
	v := failsToStart("restart-report")
	m := testManager(t, v)

	if err := m.restartTargets([]core.VPNItem{v}); err == nil {
		t.Fatal("a restart that brought nothing back reported success")
	}
}

// Both restarting branches of the autostart check act on the saved target and
// say in the log that they did.
func TestEveryAutostartRestartBranchRestarts(t *testing.T) {
	t.Run("force restart", func(t *testing.T) {
		v := failsToStart("force-branch")
		m := testManager(t, v)
		m.saveStartedList([]string{v.Name})

		if err := m.AutostartCheck(true, false); err != nil {
			t.Fatalf("AutostartCheck: %v", err)
		}
		if log := covMgrLog(t, m); !strings.Contains(log, "did not come back after the force restart") {
			t.Errorf("the force restart did not run:\n%s", log)
		}
	})

	t.Run("per-VPN failure", func(t *testing.T) {
		// A sing-box target that is not running is the one failure this branch
		// reaches without a single HTTP probe.
		v := core.VPNItem{Name: "zz-test-per-vpn-branch", Type: core.VPNSingbox}
		m := testManager(t, v)
		m.saveStartedList([]string{v.Name})

		if err := m.AutostartCheck(false, false); err != nil {
			t.Fatalf("AutostartCheck: %v", err)
		}
		if log := covMgrLog(t, m); !strings.Contains(log, "not running -> will restart") {
			t.Errorf("the stopped target was not restarted:\n%s", log)
		}
	})
}

// Losing the start lock means another process is already bringing this tunnel
// up. Retrying only loses the same race again, slower — and the retry loop's
// backoff would hold the batch for nothing.
func TestALostStartLockIsNotRetried(t *testing.T) {
	v := failsToStart("lock-race")
	m := testManager(t, v)

	release, ok := m.lockStart(v.Name)
	if !ok {
		t.Fatal("could not take the lock on a free VPN")
	}
	defer release()

	start := time.Now()
	err := m.startOneWithRetry(v, 3)
	if !errors.Is(err, errStartInProgress) {
		t.Fatalf("startOneWithRetry = %v, want errStartInProgress", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("gave up after %s; it went through the retry backoff instead of returning at once", elapsed)
	}
}

// The bug this MR's own fix introduced, and the reason a raced start is reported
// separately rather than folded into either of the other two outcomes.
//
// The sing-box update restarts a VPN as Stop-then-Start, and
// Stop takes the name off the saved list on disk first. If the Start that
// follows loses the lock race to the daemon healing the same tunnel, this call
// brings nothing up — and rebuilding the list from what it started alone would
// drop a tunnel that is running out of the set an autostart restores, until
// someone noticed and started it by hand.
func TestARacedStartStaysOnTheStartedList(t *testing.T) {
	v := failsToStart("raced-start")
	m := testManager(t, v)

	// What Stop already did: the name is gone from the list on disk.
	m.saveStartedList(nil)

	// What the other process is doing: holding this VPN's start lock.
	release, ok := m.lockStart(v.Name)
	if !ok {
		t.Fatal("could not take the lock on a free VPN")
	}
	defer release()

	if err := m.startTargets([]core.VPNItem{v}); err != nil {
		t.Fatalf("a start left to another process reported failure: %v", err)
	}

	got := m.loadStartedList()
	if len(got) != 1 || got[0] != v.Name {
		t.Errorf("started list = %v, want %s back on it", got, v.Name)
	}
}
