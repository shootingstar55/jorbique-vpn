package vpnmgr

import (
	"path/filepath"
	"testing"

	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/macos/logger"
)

func TestEmptyTo(t *testing.T) {
	if got := emptyTo("", "none"); got != "none" {
		t.Errorf("emptyTo(\"\") = %q, want none", got)
	}
	if got := emptyTo("wifi", "none"); got != "wifi" {
		t.Errorf("emptyTo(\"wifi\") = %q, want wifi", got)
	}
}

// TestHandlePowerWillSleep verifies the sleep handler records the sleeping state,
// which holds scheduled checks until the wake.
func TestHandlePowerWillSleep(t *testing.T) {
	m := &Manager{Logger: logger.New(filepath.Join(t.TempDir(), "app.log"))}

	m.handlePowerWillSleep()

	m.autoMu.Lock()
	defer m.autoMu.Unlock()
	if !m.isSystemSleeping {
		t.Error("isSystemSleeping should be true after will-sleep")
	}
}

// Turning boot autostart on used to mean restarting the daemon so it would
// re-read the setting. The request that turns it on is served *by* the daemon,
// so the restart killed the process performing it and left the machine with no
// daemon at all. The setting is read per event instead, so nothing restarts.
func TestHealingIsDecidedPerEventNotAtStartup(t *testing.T) {
	dir := t.TempDir()
	p := &core.Paths{BinDir: core.InstalledBinDir, DataDir: dir, CfgDir: filepath.Join(dir, "cfg"), LogDir: dir}
	m := &Manager{Paths: p}

	if m.healingWanted() {
		t.Fatal("healing without anyone asking for it")
	}
	if err := core.SetBootAutostart(p, true); err != nil {
		t.Fatal(err)
	}
	if !m.healingWanted() {
		t.Error("the setting was turned on and the running daemon did not notice")
	}
	if err := core.SetBootAutostart(p, false); err != nil {
		t.Fatal(err)
	}
	if m.healingWanted() {
		t.Error("the setting was turned off and the running daemon did not notice")
	}
}
