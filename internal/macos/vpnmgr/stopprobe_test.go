package vpnmgr

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/macos/logger"
)

// newStopManager builds a Manager over a temp dir with a single singbox-typed
// VPN. It is left without a NetworkSetup so the DNS work stopOne now does for
// sing-box too stops at the interface and never touches the real system; the
// name must not collide with a real profile because pidFileCandidatesForVPN
// also scans the install's run directories.
func newStopManager(t *testing.T) (*Manager, core.VPNItem, string) {
	t.Helper()
	dir := t.TempDir()
	started := filepath.Join(dir, "started.list")
	v := core.VPNItem{Name: "zz-stop-test-vpn", Type: core.VPNSingbox, File: filepath.Join(dir, "zz-stop-test-vpn.json")}
	if err := os.WriteFile(v.File, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	m := &Manager{
		Paths:  &core.Paths{RunDir: dir, LogDir: dir, CfgDir: dir},
		Cfg:    &core.AppConfig{VPNs: []core.VPNItem{v}, StartedListPath: &started},
		Logger: logger.New(filepath.Join(dir, "app.log")),
	}
	return m, v, dir
}

// spawnUnkillable starts a child that ignores SIGTERM, so Terminate spends its
// full grace period on it. Returns its pid; the child is SIGKILLed at cleanup.
func spawnUnkillable(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", "trap '' TERM; sleep 30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	return cmd.Process.Pid
}

// A stop must make the VPN look stopped immediately, not after the terminate
// grace: everything that decides whether to probe the check host — `status`,
// the menu bar, the daemon — reads the pid file, and a probe fired inside that
// window reported a FAIL seconds after the user pressed Stop.
func TestStopOneClearsPIDFileBeforeTerminating(t *testing.T) {
	m, v, dir := newStopManager(t)
	pidFile := filepath.Join(dir, v.Name+".pid")
	pid := spawnUnkillable(t)
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		t.Fatalf("write pid file: %v", err)
	}
	if !m.IsRunning(v) {
		t.Fatal("precondition: the VPN should look running")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		m.stopOne(v)
	}()

	// stopOne blocks for the 3s grace (the child ignores SIGTERM), so anything
	// observed well before that happened before the terminate finished.
	deadline := time.Now().Add(1500 * time.Millisecond)
	for {
		if _, err := os.Stat(pidFile); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pid file still present 1.5s into the stop: status would still probe this VPN's host")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if m.IsRunning(v) {
		t.Error("IsRunning is still true after the pid file was dropped")
	}

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("stopOne did not return")
	}
}

// Stop must leave the started list before it stops: between dropping the pid
// file and rewriting the list, a VPN looks "not running but wanted", which is
// exactly what the daemon's autostart check restarts.
func TestStopLeavesStartedListBeforeStopping(t *testing.T) {
	m, v, dir := newStopManager(t)
	m.saveStartedList([]string{v.Name})
	pidFile := filepath.Join(dir, v.Name+".pid")
	pid := spawnUnkillable(t)
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		m.Stop(v.Name)
	}()

	deadline := time.Now().Add(1500 * time.Millisecond)
	for {
		if len(m.loadStartedList()) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("still on the started list 1.5s into the stop: the daemon could start it right back up")
		}
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Stop did not return")
	}
	if got := m.loadStartedList(); len(got) != 0 {
		t.Errorf("started list after Stop: %v", got)
	}
}

// Stopping a VPN that is not running is a no-op that still takes it off the
// started list.
func TestStopNotRunning(t *testing.T) {
	m, v, _ := newStopManager(t)
	m.saveStartedList([]string{v.Name})
	m.Stop(v.Name)
	if got := m.loadStartedList(); len(got) != 0 {
		t.Errorf("started list after stopping an idle VPN: %v", got)
	}
}

// Stopping reads no profile, so a VPN whose file has gone is stopped all the
// same. The stop picker lists what is running, file or not, and names it here;
// a Stop that skipped it left the tunnel up and on the started list.
func TestStopWithoutProfileFile(t *testing.T) {
	m, v, dir := newStopManager(t)
	if err := os.Remove(v.File); err != nil {
		t.Fatal(err)
	}
	m.saveStartedList([]string{v.Name})
	cmd := exec.Command("/bin/sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	exited := make(chan struct{})
	go func() {
		_, _ = cmd.Process.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-exited
	})
	pidFile := filepath.Join(dir, v.Name+".pid")
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	m.Stop(v.Name)

	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("still running after Stop")
	}
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Errorf("pid file left behind: %v", err)
	}
	if got := m.loadStartedList(); len(got) != 0 {
		t.Errorf("started list after Stop: %v", got)
	}
}
