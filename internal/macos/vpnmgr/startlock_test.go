package vpnmgr

import (
	"path/filepath"
	"testing"

	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/macos/logger"
)

// The same VPN must not be started by two processes at once: the menu bar's
// start runs as its own process while the daemon's autostart-check starts
// tunnels inside the daemon, and neither could see the other.
func TestOnlyOneStartOfAVPNAtATime(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{
		Paths:  &core.Paths{RunDir: dir, LogDir: dir},
		Logger: logger.New(filepath.Join(dir, "app.log")),
	}

	release, ok := m.lockStart("intra")
	if !ok {
		t.Fatal("could not take the lock on a free VPN")
	}
	if _, ok := m.lockStart("intra"); ok {
		t.Error("a second start of the same VPN got the lock")
	}
	// A different VPN is not the one being started, and must not have to wait.
	otherRelease, ok := m.lockStart("projects")
	if !ok {
		t.Error("a different VPN could not start while intra was starting")
	} else {
		otherRelease()
	}

	release()
	release2, ok := m.lockStart("intra")
	if !ok {
		t.Fatal("the lock was not free again after the start finished")
	}
	release2()
}
