package paths

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jorbiquevpn/internal/constants"
)

func TestDataDir(t *testing.T) {
	t.Setenv("APPDATA", filepath.FromSlash("/tmp/appdata"))
	if got := DataDir(); got != filepath.Join("/tmp/appdata", constants.AppDataDirName) {
		t.Errorf("DataDir = %q", got)
	}
	// Empty APPDATA falls back to the user home directory.
	t.Setenv("APPDATA", "")
	if got := DataDir(); !strings.HasSuffix(got, constants.AppDataDirName) {
		t.Errorf("fallback DataDir = %q", got)
	}
}

func TestTempAppDir(t *testing.T) {
	if got := TempAppDir(); filepath.Base(got) != constants.TempDirName {
		t.Errorf("TempAppDir = %q", got)
	}
}

func TestRuntimeConfigPath(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	p := RuntimeConfigPath()
	if filepath.Base(p) != "runtime_config.json" {
		t.Errorf("RuntimeConfigPath = %q", p)
	}
	if st, err := os.Stat(filepath.Dir(p)); err != nil || !st.IsDir() {
		t.Errorf("data dir not created: %v", err)
	}
}

func TestEnsureDataDirs(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	if err := EnsureDataDirs(); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"logs", "cache", filepath.Join("cache", "tun"), "webview"} {
		if st, err := os.Stat(filepath.Join(DataDir(), sub)); err != nil || !st.IsDir() {
			t.Errorf("missing dir %s: %v", sub, err)
		}
	}
}

func TestWriteInstallVersionAndReset(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())

	// Empty version is a no-op.
	if err := WriteInstallVersion("  "); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(DataDir(), constants.InstallVersionFileName)); err == nil {
		t.Error("blank version should not write a marker")
	}

	if err := WriteInstallVersion("2.0.0"); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(DataDir(), constants.InstallVersionFileName)
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("marker not written: %v", err)
	}

	// Matching version -> data dir is preserved.
	probe := filepath.Join(DataDir(), "keepme.txt")
	if err := os.WriteFile(probe, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	ResetDataDirsIfVersionMismatch("2.0.0")
	if _, err := os.Stat(probe); err != nil {
		t.Error("matching version should not wipe data dir")
	}

	// An application upgrade keeps the independently updated core and browser profile.
	cacheDir := filepath.Join(DataDir(), "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatal(err)
	}
	// What the person configured, on the other hand, survives the upgrade.
	store := filepath.Join(DataDir(), StoreFileName)
	draft := filepath.Join(DataDir(), VpnFormDraftFileName)
	instance := filepath.Join(DataDir(), TelemetryInstanceIDFileName)
	engine := filepath.Join(DataDir(), constants.ProcessExeName)
	cronet := filepath.Join(DataDir(), "libcronet.dll")
	cronetVersion := filepath.Join(DataDir(), "libcronet.version")
	webview := filepath.Join(DataDir(), "webview", "profile")
	if err := os.MkdirAll(filepath.Dir(webview), 0755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{store, draft, instance, engine, cronet, cronetVersion, webview} {
		if err := os.WriteFile(f, []byte(`{"user_uuid":"mine"}`), 0644); err != nil {
			t.Fatal(err)
		}
	}

	ResetDataDirsIfVersionMismatch("9.9.9")
	if _, err := os.Stat(probe); err == nil {
		t.Error("version mismatch should wipe data dir")
	}
	if _, err := os.Stat(cacheDir); err == nil {
		t.Error("version mismatch should wipe the cache the old release wrote")
	}
	if _, err := os.Stat(draft); err == nil {
		t.Error("version mismatch should drop the unsaved VPN Config draft an older release left")
	}
	for _, f := range []string{store, instance, engine, cronet, cronetVersion, webview} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Errorf("upgrade lost %s: %v", filepath.Base(f), err)
			continue
		}
		if string(b) != `{"user_uuid":"mine"}` {
			t.Errorf("upgrade rewrote %s: %s", filepath.Base(f), b)
		}
	}

	// Empty wantVersion is a no-op.
	ResetDataDirsIfVersionMismatch("")
}
