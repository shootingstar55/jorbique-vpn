package autoconnect

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Progress is safe to show to an unprivileged UI: no credentials, hostnames,
// protocol controls, or engine errors. Completed counts finished real tests.
type Progress struct {
	Phase     string `json:"phase"`
	Completed int    `json:"completed"`
	Total     int    `json:"total"`
}

const ProgressFile = "connection-check.json"

// WriteProgress publishes only display information to the macOS menu-bar app.
// The run directory belongs to the daemon; the public-readable file contains
// only phase and counts and is never read as privileged configuration.
func WriteProgress(runDir string, progress Progress) {
	data, err := json.Marshal(progress)
	if err != nil {
		return
	}
	writePublic(runDir, ProgressFile, data)
}

// writePublic replaces runDir/name with data, readable by every user.
func writePublic(runDir, name string, data []byte) {
	file, err := os.CreateTemp(runDir, "."+name+"-")
	if err != nil {
		return
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return
	}
	if err := file.Chmod(0644); err != nil {
		_ = file.Close()
		return
	}
	if err := file.Close(); err != nil {
		return
	}
	_ = os.Rename(file.Name(), filepath.Join(runDir, name))
}
