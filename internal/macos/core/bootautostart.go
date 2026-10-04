package core

import (
	"os"
	"path/filepath"
)

// The LaunchDaemon used to exist only when the user asked for boot autostart:
// installing it was the feature. It is now the client's privileged component
// and is installed with the client, so its presence no longer means anything
// about autostart — this file does.
//
// Without the marker the daemon serves the control socket and nothing else: no
// tunnels brought up at boot, no watchers, exactly what a machine that never
// asked for autostart did before.

// BootAutostartMarker is the file that records the user's choice. It lives with
// the configuration, so it is root-owned like everything else there.
const BootAutostartMarker = "boot-autostart"

// BootAutostartPath is where the marker lives.
func BootAutostartPath(p *Paths) string {
	if p == nil {
		return ""
	}
	return filepath.Join(p.StateDir(), BootAutostartMarker)
}

// BootAutostartEnabled reports whether tunnels should come back at boot.
func BootAutostartEnabled(p *Paths) bool {
	path := BootAutostartPath(p)
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// SetBootAutostart records or clears the choice. Root's to write, like the rest
// of the state directory.
func SetBootAutostart(p *Paths, enabled bool) error {
	path := BootAutostartPath(p)
	if path == "" {
		return BadConfig("no state directory for the autostart marker")
	}
	if !enabled {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("boot autostart requested\n"), 0o644)
}
