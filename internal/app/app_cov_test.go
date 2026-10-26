package app

import (
	"errors"
	"jorbiquevpn/internal/autoconnect"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"jorbiquevpn/internal/paths"
)

// logToTestDir points the controller log at the app's data dir and returns a
// reader for it.
func logToTestDir(t *testing.T, a *App) func() string {
	t.Helper()
	controllerLog(a.dataDir, false)
	return func() string {
		data, err := os.ReadFile(filepath.Join(a.dataDir, "logs", controllerLogFile))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
}

// notADirectory returns a path below a regular file, so creating or writing
// anything there fails.
func notADirectory(t *testing.T) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(file, "sub")
}

func TestApp_TunnelStatesReportsTheCore(t *testing.T) {
	a := newTestApp(t)
	if got := a.tunnelStates(); !reflect.DeepEqual(got, map[string]bool{"sing-box": false}) {
		t.Errorf("tunnelStates = %v", got) // core.IsRunning is false on the stub platform
	}
}

// Telemetry keeps this install's ID in the data dir; with no UUID stored it
// sends nothing.
func TestApp_StartTelemetryStoresInstallID(t *testing.T) {
	a := newTestApp(t)
	a.StartTelemetry()
	if data, err := os.ReadFile(filepath.Join(a.dataDir, paths.TelemetryInstanceIDFileName)); err != nil || strings.TrimSpace(string(data)) == "" {
		t.Fatalf("install ID not stored: %q, %v", data, err)
	}
}

func TestApp_ActivateUserUUIDSaveFails(t *testing.T) {
	a := newTestApp(t)
	a.dataDir = notADirectory(t)
	answerUUIDChecks(t, nil)
	msg, err := a.ActivateUserUUID(appTestUUID)
	if !errors.Is(err, ErrConfigUpdate) || msg != "Failed to save config" {
		t.Fatalf("ActivateUserUUID = %q, %v; want the save failure", msg, err)
	}
}

func TestControllerLogWarnsWhenDataDirsFail(t *testing.T) {
	a := newTestApp(t)
	t.Setenv("APPDATA", notADirectory(t))
	readLog := logToTestDir(t, a)
	controllerLog(a.dataDir, true)
	if !strings.Contains(readLog(), "Could not ensure data directories") {
		t.Errorf("no warning in the log:\n%s", readLog())
	}
}

// Automatic choices ignore retired/manual toggles and unverified old drafts.
func TestApp_AutomaticBuildUsesSavedUUIDAndMeasuredChoices(t *testing.T) {
	a := newTestApp(t)
	t.Setenv("APPDATA", t.TempDir())
	a.store.SetUserUUIDAndOutbounds(appTestUUID, map[string]bool{"retired": true, "proxy": false})
	a.store.SetNaiveMode(true)
	a.store.SelectedNode = "retired"
	if err := a.SaveVpnFormDraft("22222222-2222-4222-8222-222222222222", "", nil); err != nil {
		t.Fatal(err)
	}
	profile := []byte(`{"outbounds":[{"type":"direct","tag":"direct"},{"type":"vless","tag":"proxy","uuid":""},{"type":"urltest","tag":"auto","outbounds":["proxy"]}],"route":{"rules":[],"final":"auto"}}`)
	path, err := a.buildRuntimeConfig(profile, autoconnect.Result{Working: []autoconnect.Measurement{{Tag: "proxy"}}})
	if err != nil {
		t.Fatal(err)
	}
	obs := builtOutbounds(t, path)
	if obs["proxy"]["uuid"] != appTestUUID || obs["auto"]["type"] != "urltest" || !reflect.DeepEqual(obs["auto"]["outbounds"], []any{"proxy"}) {
		t.Fatal(obs)
	}
}

// Disconnect's cleanup removes the session marker and the running-profile file.
func TestApp_CleanupTunnelArtifactsRemovesSessionFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("removes NRPT rules on Windows")
	}
	a := newTestApp(t)
	for _, name := range []string{".jorbique_vpn_state.json", currentProfileFile} {
		if err := os.WriteFile(filepath.Join(a.dataDir, name), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if !a.staleTunnelArtifactsPresent(a.dataDir) {
		t.Fatal("artifacts not detected")
	}
	a.cleanupTunnelArtifacts(a.dataDir, func(string, string) {})
	if a.staleTunnelArtifactsPresent(a.dataDir) {
		t.Error("session files survived the cleanup")
	}
}
