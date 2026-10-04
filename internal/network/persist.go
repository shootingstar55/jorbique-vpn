package network

import (
	"os"
	"path/filepath"
)

const stateFileName = ".jorbique_vpn_state.json"

// The state file marks that a tunnel is up, and nothing more: sing-box takes the
// routes itself (auto_route), and everything goes through the one sing-tun
// interface. Only the file's existence is consulted.
func SaveState(projectRoot string) {
	_ = os.WriteFile(filepath.Join(projectRoot, stateFileName), []byte("{}"), 0644)
}

// LoadState reports whether a tunnel left its marker behind.
func LoadState(projectRoot string) bool {
	_, err := os.Stat(filepath.Join(projectRoot, stateFileName))
	return err == nil
}

// ClearState removes the state file
func ClearState(projectRoot string) {
	os.Remove(filepath.Join(projectRoot, stateFileName))
}
