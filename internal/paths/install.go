package paths

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"jorbiquevpn/internal/constants"
)

// The files under DataDir that hold what the person configured rather than what
// the installer put there: the UUID, the outbound toggles, the direct domains
// and the ignored applications live in the store, and the modal's unsaved input
// lives in the draft beside it. The install ID is how the server tells this
// machine from the user's others; a new one per upgrade would count it as a new
// device every release. They are named here because this package is also the
// one that clears the directory, and the two have to agree.
const (
	StoreFileName               = "servers.json"
	VpnFormDraftFileName        = "vpn_form_draft.json"
	TelemetryInstanceIDFileName = "telemetry_instance_id"
)

// TempAppDir returns %TEMP%/JorbiqueVPN (same root as RemoteSingboxCachePath).
func TempAppDir() string {
	return filepath.Join(os.TempDir(), constants.TempDirName)
}

// ResetDataDirsIfVersionMismatch clears DataDir and TempAppDir when the install marker
// is missing or does not match wantVersion (new binary or first run after old builds without marker).
//
// Everything a release owns is thrown away, because that is what the reset is
// for: the extracted sing-box, the generated runtime config, the caches and the
// logs all belong to the version that wrote them. What the person configured is
// not part of the install and stays — this used to remove the whole directory,
// so every upgrade handed the user a client with no UUID, no direct domains and
// no ignored applications, and they had to type it all in again.
func ResetDataDirsIfVersionMismatch(wantVersion string) {
	wantVersion = strings.TrimSpace(wantVersion)
	if wantVersion == "" {
		return
	}
	dataDir := DataDir()
	b, err := os.ReadFile(filepath.Join(dataDir, constants.InstallVersionFileName))
	if err == nil && strings.TrimSpace(string(b)) == wantVersion {
		return
	}
	clearExcept(dataDir, StoreFileName, VpnFormDraftFileName, TelemetryInstanceIDFileName)
	_ = os.RemoveAll(TempAppDir())
}

// clearExcept empties dir, leaving the named entries where they are.
func clearExcept(dir string, keep ...string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if slices.Contains(keep, e.Name()) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(dir, e.Name()))
	}
}

// WriteInstallVersion writes the marker so subsequent runs of the same binary skip wiping.
func WriteInstallVersion(wantVersion string) error {
	wantVersion = strings.TrimSpace(wantVersion)
	if wantVersion == "" {
		return nil
	}
	dataDir := DataDir()
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dataDir, constants.InstallVersionFileName)
	return os.WriteFile(path, []byte(wantVersion+"\n"), 0644)
}
