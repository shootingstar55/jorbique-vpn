package vpnmgr

import (
	"path/filepath"
	"testing"

	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/macos/logger"
)

func newRestartManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	// The VPN name must not exist as a real profile: runningVPNPID also scans
	// the real install's run directories for "<name>.pid", so a test Manager
	// configured with a production name (jorbique) would see — and
	// restart — the developer's actual tunnel.
	return &Manager{
		Paths: &core.Paths{RunDir: dir, LogDir: dir, CfgDir: dir},
		Cfg: &core.AppConfig{Singbox: core.SingboxSection{UUID: testUserUUID}, VPNs: []core.VPNItem{
			{Name: "zz-restart-test-vpn", Type: core.VPNSingbox, File: filepath.Join(dir, "zz-restart-test-vpn.json")},
		}},
		Logger: logger.New(filepath.Join(dir, "app.log")),
	}
}

func TestRestartUnknownName(t *testing.T) {
	m := newRestartManager(t)
	if err := m.Restart([]string{"bogus"}); err == nil {
		t.Fatal("expected an error for an unknown VPN name")
	}
}

func TestRestartAllWithNothingRunning(t *testing.T) {
	m := newRestartManager(t)
	// No pid files exist, so "all" resolves to zero running VPNs; Restart must
	// be a calm no-op rather than an error or a start of everything configured.
	if err := m.Restart([]string{"all"}); err != nil {
		t.Fatalf("Restart(all) with nothing running: %v", err)
	}
}
