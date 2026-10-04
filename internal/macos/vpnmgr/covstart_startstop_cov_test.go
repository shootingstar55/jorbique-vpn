package vpnmgr

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jorbiquevpn/internal/constants"
	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/macos/logger"
)

// covStartProfile is the smallest downloaded profile the builder accepts: one
// proxy behind a selector, and no DoH resolver to probe.
const covStartProfile = `{
  "outbounds": [
    {"type": "vless", "tag": "vless-reality", "server": "example.org", "server_port": 443, "uuid": ""},
    {"type": "selector", "tag": "proxy-selector", "outbounds": ["vless-reality"], "default": "vless-reality"},
    {"type": "direct", "tag": "direct"}
  ],
  "route": {"rules": [], "final": "proxy-selector"}
}`

// covStartManager is a Manager whose start gets as far as spawning sing-box and
// no further: the log directory sits under a regular file, so the spawn cannot
// open the core's log and returns before any process (or sudo) runs.
// The profile URL points at a closed loopback port for the test's duration:
// the download fails at once and the start falls back to the cached profile,
// whether or not the machine (or CI runner) can reach the real server.
func covStartManager(t *testing.T) (*Manager, core.VPNItem) {
	t.Helper()
	oldURL := constants.SingboxClientURL
	constants.SingboxClientURL = "http://" + covMgrDeadHost(t) + "/singbox_client.json"
	t.Cleanup(func() { constants.SingboxClientURL = oldURL })
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "cfg")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	sbPath := filepath.Join(cfgDir, "singbox.json")
	if err := os.WriteFile(sbPath, []byte(`{"log":{"level":"warn"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	missingExe := filepath.Join(dir, "no-such-sing-box")
	v := core.VPNItem{Name: "zz-test-cov-start", Type: core.VPNSingbox, File: sbPath, Exec: &missingExe}
	m := &Manager{
		Paths: &core.Paths{
			BinDir: dir, RunDir: dir, CfgDir: cfgDir, CfgFile: filepath.Join(cfgDir, "vpn_cfg.json"),
			LogDir: filepath.Join(blocker, "logs"), SingboxFallbackFile: "singbox.json",
		},
		Cfg:    &core.AppConfig{Singbox: core.SingboxSection{UUID: testUserUUID}, VPNs: []core.VPNItem{v}},
		Logger: logger.New(filepath.Join(dir, "app.log")),
	}
	return m, v
}

func covStartWriteProfile(t *testing.T, m *Manager) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(m.Paths.CfgDir, "singbox_client.json"), []byte(covStartProfile), 0o644); err != nil {
		t.Fatal(err)
	}
}

// covStartLog reads the app.log a test manager writes in dir.
func covStartLog(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "app.log"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// With the server unreachable the start builds from the cached profile, writes
// a config that keeps a DNS cache per profile and carries the user's UUID, and
// only then fails — at the spawn, which is as far as a test may go.
func TestCovStartStartOneWritesTheConfigBeforeSpawning(t *testing.T) {
	for _, tc := range []struct {
		name, fallback, routeLog string
	}{
		{"route rules re-applied", "singbox.json", "Re-applied route rules"},
		// The fallback names the config directory itself, which cannot be read
		// as a config: the patch fails and the start goes on regardless.
		{"route rules refused", "", "Could not re-apply route rules"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, v := covStartManager(t)
			m.Paths.SingboxFallbackFile = tc.fallback
			covStartWriteProfile(t, m)

			err := m.startOne(v)
			if err == nil || !strings.Contains(err.Error(), "open log file failed") {
				t.Fatalf("startOne = %v, want the spawn to fail on the log file", err)
			}

			root := map[string]any{}
			data, rerr := os.ReadFile(v.File)
			if rerr != nil {
				t.Fatal(rerr)
			}
			if err := json.Unmarshal(data, &root); err != nil {
				t.Fatalf("written config is not JSON: %v\n%s", err, data)
			}
			experimental, _ := root["experimental"].(map[string]any)
			cache, _ := experimental["cache_file"].(map[string]any)
			wantCache := filepath.Join(m.Paths.BinDir, "cache", "zz_test_cov_start", "cache.db")
			if cache["path"] != wantCache || cache["cache_id"] != "zz_test_cov_start" || cache["store_dns"] != true {
				t.Errorf("cache_file = %v, want path %s", cache, wantCache)
			}
			if _, err := os.Stat(filepath.Dir(wantCache)); err != nil {
				t.Errorf("cache directory not created: %v", err)
			}
			obs, _ := root["outbounds"].([]any)
			proxy, _ := obs[0].(map[string]any)
			if proxy["uuid"] != testUserUUID {
				t.Errorf("outbound %v, want the user's UUID filled in", proxy)
			}

			log := covStartLog(t, m.Paths.BinDir)
			for _, want := range []string{"starting from the cached copy", "Updated sing-box config from remote template", tc.routeLog} {
				if !strings.Contains(log, want) {
					t.Errorf("log is missing %q:\n%s", want, log)
				}
			}
		})
	}
}

// No server and no cached profile: nothing to build from, and the start says so
// before writing anything.
func TestCovStartStartOneWithoutAProfile(t *testing.T) {
	m, v := covStartManager(t)
	before, _ := os.ReadFile(v.File)

	err := m.startOne(v)
	if err == nil || !strings.Contains(err.Error(), "template download") {
		t.Fatalf("startOne = %v, want the template download error", err)
	}
	if after, _ := os.ReadFile(v.File); string(after) != string(before) {
		t.Errorf("a failed build rewrote the config:\n%s", after)
	}
}

func TestCovStartStartOneCannotCreateTheConfigDirectory(t *testing.T) {
	m, v := covStartManager(t)
	v.File = filepath.Join(m.Paths.BinDir, "not-a-dir", "cfg", "singbox.json")

	err := m.startOne(v)
	if err == nil || !strings.Contains(err.Error(), "cannot create the config directory for "+v.Name) {
		t.Fatalf("startOne = %v, want the config directory error", err)
	}
}

func TestCovStartSingboxCommand(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "my-sing-box")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing-sing-box")
	plain := "sing-box"
	const sbPath = "/cfg/singbox.json"

	for _, tc := range []struct {
		name    string
		bundled string
		exec    *string
		args    []string
		want    []string
	}{
		{"bundled binary wins for the default name", "/opt/bundled/sing-box", nil, nil, []string{"/opt/bundled/sing-box", "run", "-c", sbPath}},
		{"the default name spelled out", "/opt/bundled/sing-box", &plain, nil, []string{"/opt/bundled/sing-box", "run", "-c", sbPath}},
		{"an executable the profile names", "/opt/bundled/sing-box", &exe, []string{"check"}, []string{exe, "check"}},
		{"a path that is not there is used as given", "", &missing, nil, []string{missing, "run", "-c", sbPath}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &Manager{singboxExe: tc.bundled}
			got := m.singboxCommand(core.VPNItem{Name: "zz-cmd", Exec: tc.exec, Args: tc.args}, sbPath)
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Errorf("command = %q, want %q", got, tc.want)
			}
		})
	}

	// Nothing bundled: the binary is looked up, and whatever is found (or not)
	// is still sing-box run against the config.
	got := (&Manager{}).singboxCommand(core.VPNItem{Name: "zz-cmd"}, sbPath)
	if len(got) != 4 || filepath.Base(got[0]) != "sing-box" || strings.Join(got[1:], " ") != "run -c "+sbPath {
		t.Errorf("command = %q", got)
	}
}

// A run directory that is not there leaves no lock to take; the start goes
// ahead without one rather than refusing.
func TestCovStartLockStartWithoutARunDirectory(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{
		Paths:  &core.Paths{RunDir: filepath.Join(dir, "missing")},
		Logger: logger.New(filepath.Join(dir, "app.log")),
	}
	release, ok := m.lockStart("zz-cov-lock")
	if !ok || release == nil {
		t.Fatalf("lockStart: ok = %v, release nil = %v; want a no-op release and ok", ok, release == nil)
	}
	release()
	if log := covStartLog(t, dir); !strings.Contains(log, "start lock for zz-cov-lock unavailable") {
		t.Errorf("log = %q", log)
	}
}
