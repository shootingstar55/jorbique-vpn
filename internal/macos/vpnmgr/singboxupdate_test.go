package vpnmgr

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/macos/logger"
)

func TestSingboxSemverRe(t *testing.T) {
	cases := map[string]string{
		"sing-box version 1.13.7":                   "1.13.7",
		"sing-box version 1.14.0-alpha.2+custom.1":  "1.14.0-alpha.2+custom.1",
		"sing-box version 1.13.7\n\nEnvironment...": "1.13.7",
		"v1.2.30 (built)":                           "1.2.30",
		"no version here":                           "",
		"":                                          "",
	}
	for in, want := range cases {
		got := ""
		if mt := singboxSemverRe.FindStringSubmatch(in); len(mt) > 1 {
			got = mt[1]
		}
		if got != want {
			t.Errorf("semver(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLocalSingboxVersionFindsALaterInstallAndUpdate(t *testing.T) {
	binDir := t.TempDir()
	m := &Manager{Paths: &core.Paths{BinDir: binDir}}
	if got := m.localSingboxVersion(); got != "" {
		t.Fatalf("before install: %q", got)
	}
	for _, version := range []string{"1.14.0-alpha.2", "1.14.1"} {
		if err := os.WriteFile(filepath.Join(binDir, "sing-box"), []byte("#!/bin/sh\necho 'sing-box version "+version+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if got := m.localSingboxVersion(); got != version {
			t.Fatalf("after install/update: %q, want %q", got, version)
		}
	}
}

// writeFakeSingbox writes an executable stub that prints body on `version` and
// exits with code. Returns its path. Skips on Windows (no shebang support).
func writeFakeSingbox(t *testing.T, body string, code int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub not supported on windows")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "sing-box")
	script := "#!/bin/sh\nprintf '%s\\n' \"" + body + "\"\nexit " + strconv.Itoa(code) + "\n"
	if err := os.WriteFile(exe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestLocalSingboxVersion(t *testing.T) {
	// No binary configured -> empty.
	if v := (&Manager{}).localSingboxVersion(); v != "" {
		t.Errorf("empty singboxExe should yield \"\", got %q", v)
	}

	// Non-existent path -> exec error -> empty.
	if v := (&Manager{singboxExe: "/nonexistent/sing-box"}).localSingboxVersion(); v != "" {
		t.Errorf("missing binary should yield \"\", got %q", v)
	}

	// Binary that prints a version -> parsed semver.
	exe := writeFakeSingbox(t, "sing-box version 1.13.7", 0)
	if v := (&Manager{singboxExe: exe}).localSingboxVersion(); v != "1.13.7" {
		t.Errorf("version = %q, want 1.13.7", v)
	}
	extended := writeFakeSingbox(t, "sing-box version 1.14.3-extended-2.7.3", 0)
	if v := (&Manager{singboxExe: extended}).localSingboxVersion(); v != "1.14.3-extended-2.7.3" {
		t.Errorf("extended version = %q", v)
	}

	// Binary that exits non-zero -> empty.
	bad := writeFakeSingbox(t, "boom", 1)
	if v := (&Manager{singboxExe: bad}).localSingboxVersion(); v != "" {
		t.Errorf("failing binary should yield \"\", got %q", v)
	}

	// Binary with no parseable version -> empty.
	noVer := writeFakeSingbox(t, "sing-box unknown build", 0)
	if v := (&Manager{singboxExe: noVer}).localSingboxVersion(); v != "" {
		t.Errorf("unparseable output should yield \"\", got %q", v)
	}
}

// The binary swap only concerns sing-box profiles; an entry of any other type
// is left alone.
func TestRunningSingboxVPNsIgnoresOtherTypes(t *testing.T) {
	dir := t.TempDir()
	// zz- names: pidFileCandidatesForVPN also scans the real install's run
	// directories, so a production name would pick up the developer's tunnel.
	sb := core.VPNItem{Name: "zz-update-test-singbox", Type: core.VPNSingbox}
	ovpn := core.VPNItem{Name: "zz-update-test-openvpn", Type: core.VPNType("openvpn")}
	m := &Manager{
		Paths:  &core.Paths{RunDir: dir, LogDir: dir, CfgDir: dir},
		Cfg:    &core.AppConfig{VPNs: []core.VPNItem{sb, ovpn}},
		Logger: logger.New(filepath.Join(dir, "app.log")),
	}
	// The test process stands in for both tunnels: IsRunning only asks whether
	// the pid in the file is alive.
	for _, v := range []core.VPNItem{sb, ovpn} {
		pidFile := filepath.Join(dir, v.Name+".pid")
		if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(m.runningVPNs()); got != 2 {
		t.Fatalf("precondition: %d VPNs look running, want 2", got)
	}

	got := m.runningSingboxVPNs()
	if len(got) != 1 || got[0].Name != sb.Name {
		t.Fatalf("runningSingboxVPNs() = %v, want only %s", got, sb.Name)
	}
}
