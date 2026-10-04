package vpnmgr

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/macos/logger"
)

func newCacheManager(t *testing.T, bin string) (*Manager, core.VPNItem) {
	t.Helper()
	run := t.TempDir()
	// zz- prefix: pidFileCandidatesForVPN also scans the real install's run
	// directories, so a production name would read the developer's own tunnel.
	v := core.VPNItem{Name: "zz-cache-test-vpn", Type: core.VPNSingbox, File: "cfg/zz-cache-test-vpn.json"}
	m := &Manager{
		Paths:  &core.Paths{BinDir: bin, RunDir: run, LogDir: run, CfgDir: run},
		Cfg:    &core.AppConfig{VPNs: []core.VPNItem{v}},
		Logger: logger.New(filepath.Join(run, "app.log")),
	}
	return m, v
}

// The persistent sing-box DNS cache is removed only via ClearSingboxCache
// (menu-bar Quit); stopOne must never touch it.
func TestClearSingboxCache(t *testing.T) {
	bin := t.TempDir()
	m, _ := newCacheManager(t, bin)

	if err := m.ClearSingboxCache(); err != nil {
		t.Fatalf("missing cache dir should be a no-op, got %v", err)
	}

	cacheDir := filepath.Join(bin, "cache", "zz_cache_test_vpn")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "cache.db"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.ClearSingboxCache(); err != nil {
		t.Fatalf("ClearSingboxCache: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bin, "cache")); !os.IsNotExist(err) {
		t.Fatal("cache dir survived ClearSingboxCache")
	}
}

// Like the Windows client, a running sing-box keeps its cache: it writes the
// file back out when it shuts down, so dropping it under a live tunnel only
// costs the next start its warm cache.
func TestClearSingboxCacheKeepsCacheWhileRunning(t *testing.T) {
	bin := t.TempDir()
	m, v := newCacheManager(t, bin)
	cacheDir := filepath.Join(bin, "cache", "zz_cache_test_vpn")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The test process itself is the "running sing-box": IsRunning only asks
	// whether the pid in the file is alive.
	pidFile := filepath.Join(m.Paths.RunDir, v.Name+".pid")
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := m.ClearSingboxCache(); err != nil {
		t.Fatalf("ClearSingboxCache: %v", err)
	}
	if _, err := os.Stat(cacheDir); err != nil {
		t.Fatalf("cache dropped while %s was still running: %v", v.Name, err)
	}
}

func TestSingboxCacheID(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Jorbique VPN", "jorbique_vpn"},
		{"singbox", "singbox"},
		{"  ", "singbox"},
		{"a/b", "a_b"},
	} {
		if got := singboxCacheID(tc.in); got != tc.want {
			t.Errorf("singboxCacheID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
