package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"jorbiquevpn/internal/app"
	"jorbiquevpn/internal/paths"
)

const uiTestUUID = "11111111-1111-1111-1111-111111111111"

// newUIApp builds a ui.App backed by a real app.App rooted in a temp data dir.
//
// The temp directory is redirected as well as APPDATA. app.New starts a
// background fetch of the sing-box profile, and that profile is cached under
// os.TempDir() rather than in the data directory — so without this the test
// suite downloads the real profile onto the developer's machine, where every
// later test that expects no profile then finds one.
func newUIApp(t *testing.T) *App {
	return newUIAppWithUUID(t, "")
}

// newUIAppWithUUID starts from a store that already holds userUUID, which
// saves without asking the server — a unit test cannot reach it.
func newUIAppWithUUID(t *testing.T, userUUID string) *App {
	t.Helper()
	t.Setenv("APPDATA", t.TempDir())
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir) // darwin, linux
	t.Setenv("TMP", dir)    // windows
	t.Setenv("TEMP", dir)
	if userUUID != "" {
		if err := os.MkdirAll(paths.DataDir(), 0o755); err != nil {
			t.Fatal(err)
		}
		store := []byte(`{"user_uuid":"` + userUUID + `"}`)
		if err := os.WriteFile(filepath.Join(paths.DataDir(), paths.StoreFileName), store, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return New(app.New())
}

func TestUI_ReadOnlyDelegations(t *testing.T) {
	a := newUIApp(t)

	if a.IsConnected() {
		t.Error("IsConnected should be false")
	}
	if a.IsAutostartEnabled() {
		t.Error("autostart is unsupported on this platform")
	}
	if a.IsSingboxInstalled() {
		t.Error("empty data dir has no sing-box")
	}
	if a.IsSingboxUpdateAvailable() {
		t.Error("no local sing-box => no update available")
	}
	if _, err := a.ListActiveApps(); err != nil {
		t.Errorf("ListActiveApps err = %v", err)
	}
	_ = a.GetIgnoredProcessNames()
	_ = a.GetIgnoredProcessPaths()
	_ = a.GetVpnUserConfig()
	if a.HasVpnUserConfig() {
		t.Error("fresh data dir has no VPN config")
	}
}

func TestUI_MutationDelegations(t *testing.T) {
	a := newUIAppWithUUID(t, uiTestUUID)

	if err := a.SaveVpnFormDraft(uiTestUUID, "draft.example", nil); err != nil {
		t.Fatal(err)
	}
	if err := a.SetVpnUserConfig(uiTestUUID, map[string]bool{"vless-reality": true, "tuic": true, "vless-grpc": true, "hysteria2": true, "anytls": true}); err != nil {
		t.Fatalf("SetVpnUserConfig: %v", err)
	}
	if !a.HasVpnUserConfig() {
		t.Error("config should be present after a valid save")
	}
	if err := a.SetIgnoredProcessNamesAndPaths([]string{"chrome"}, nil); err != nil {
		t.Fatal(err)
	}

	// Autostart is unsupported on this platform: error is surfaced.
	if err := a.SetAutostartEnabled(true); err == nil {
		t.Error("SetAutostartEnabled should error on this platform")
	}
}

func TestUI_ActivationDelegations(t *testing.T) {
	a := newUIApp(t)
	if a.HasUserUUID() {
		t.Fatal("a fresh data dir has no user UUID")
	}
	// A malformed UUID is refused before anything is sent.
	if err := a.ActivateUserUUID("not-a-uuid"); !errors.Is(err, app.ErrInvalidVless) {
		t.Fatalf("ActivateUserUUID(bad) = %v", err)
	}
	if !newUIAppWithUUID(t, uiTestUUID).HasUserUUID() {
		t.Fatal("a stored UUID was not seen")
	}
}

func TestUI_LogDelegations(t *testing.T) {
	a := newUIApp(t)
	// app.New() initializes controller.log, so it is listed and readable.
	names, err := a.ListLogFiles()
	if err != nil {
		t.Fatalf("ListLogFiles: %v", err)
	}
	_ = names
	if _, err := a.GetLogContent("controller.log"); err != nil {
		t.Errorf("GetLogContent: %v", err)
	}
	if err := a.ClearLogs(); err != nil {
		t.Errorf("ClearLogs: %v", err)
	}
}

func TestUI_PickExecutableWithoutContext(t *testing.T) {
	a := newUIApp(t)
	if _, err := a.PickExecutableForBypass(); err == nil {
		t.Error("PickExecutableForBypass should error before the Wails context is set")
	}
	a.SetWailsContext(context.TODO()) // exercise the setter
}

func TestUI_WrapBackendError(t *testing.T) {
	if err := wrapBackendError("", nil); err != nil {
		t.Errorf("no error should wrap to nil, got %v", err)
	}
	base := app.ErrConfigUpdate
	wrapped := wrapBackendError("user message", base)
	if !errors.Is(wrapped, base) {
		t.Error("wrapBackendError should preserve the sentinel")
	}
	// Error without a user message returns the bare error.
	if err := wrapBackendError("", base); !errors.Is(err, base) {
		t.Error("bare error not preserved")
	}
}
