//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"jorbiquevpn/internal/constants"
	"jorbiquevpn/internal/server"
)

func TestApp_GetSingboxLocalVersion_Integration(t *testing.T) {
	a := &App{dataDir: t.TempDir(), store: &server.Store{}}

	// No binary installed -> empty version.
	if v := a.GetSingboxLocalVersion(); v != "" {
		t.Errorf("expected empty version with no binary, got %q", v)
	}

	exePath := filepath.Join(a.dataDir, constants.ProcessExeName)
	script := "#!/bin/sh\necho 'sing-box version 1.13.7 (integration)'\n"
	if err := os.WriteFile(exePath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	if v := a.GetSingboxLocalVersion(); v != "1.13.7" {
		t.Errorf("parsed version = %q, want 1.13.7", v)
	}
	// Second call hits the mtime/path cache and returns the same value.
	if v := a.GetSingboxLocalVersion(); v != "1.13.7" {
		t.Errorf("cached version = %q, want 1.13.7", v)
	}
	if err := os.WriteFile(exePath, []byte("#!/bin/sh\necho 'sing-box version 1.14.0-alpha.2+custom.1'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Second)
	if err := os.Chtimes(exePath, future, future); err != nil {
		t.Fatal(err)
	}
	if v := a.GetSingboxLocalVersion(); v != "1.14.0-alpha.2+custom.1" {
		t.Errorf("updated version = %q", v)
	}
}

// TestApp_PersistenceRoundTrip is an end-to-end check across app + server + config:
// saving the VPN config writes servers.json, which a fresh Store load reads back.
func TestApp_PersistenceRoundTrip(t *testing.T) {
	// Builds its Apps directly rather than through newTestApp, so it has to
	// redirect the profile cache itself; HasVpnUserConfig reads it.
	isolateProfileCache(t)
	dir := t.TempDir()
	a := &App{dataDir: dir, store: &server.Store{}}

	if _, err := a.SetVpnUserConfig(appTestUUID, allOutboundsEnabled()); err != nil {
		t.Fatalf("SetVpnUserConfig: %v", err)
	}

	// A brand-new App loading the same data dir sees the persisted config.
	store, err := server.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	reopened := &App{dataDir: dir, store: store}
	if !reopened.HasVpnUserConfig() {
		t.Error("reopened app should report a saved VPN config")
	}
	form := reopened.GetVpnUserConfig()
	if form.UUID != appTestUUID {
		t.Errorf("reopened UUID = %q", form.UUID)
	}
	if got := reopened.GetIgnoredProcessNames(); got == nil {
		_ = got // ignored names default empty; just exercising the path
	}
}
