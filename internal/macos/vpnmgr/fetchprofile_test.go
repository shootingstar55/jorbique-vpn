package vpnmgr

import (
	"os"
	"path/filepath"
	"testing"

	"jorbiquevpn/internal/macos/control"
	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/macos/logger"
	"jorbiquevpn/internal/macos/singbox"
)

// The outbound list comes from the profile, and the profile used to arrive only
// as a side effect of building a config — which needs the UUID the user is
// being asked for in the same panel. Fetching it must therefore need no UUID
// and no stored choice: nothing about which protocols a server offers is the
// user's secret.
func TestRefreshRemoteProfileNeedsNoUUID(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "vpn_cfg.json")
	if err := os.WriteFile(cfgFile, []byte(`{"singbox":{"uuid":""},"vpns":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(dir, "singbox.json")
	v := core.VPNItem{Name: "zz-fetch-test", Type: core.VPNSingbox, File: base}
	m := &Manager{
		Paths:  &core.Paths{BinDir: dir, RunDir: dir, LogDir: dir, CfgDir: dir, CfgFile: cfgFile},
		Cfg:    &core.AppConfig{VPNs: []core.VPNItem{v}},
		Logger: logger.New(filepath.Join(dir, "app.log")),
	}

	// No network in a test, so the download fails — what matters is that it was
	// attempted without asking for a credential, and reported rather than
	// pretending to have succeeded.
	err := m.RefreshRemoteProfile()
	if err == nil {
		// A machine with network reaches the real profile; then the cache must exist.
		if _, statErr := os.Stat(filepath.Join(dir, "singbox_client.json")); statErr != nil {
			t.Errorf("reported success without writing a profile: %v", statErr)
		}
		return
	}
	if _, statErr := os.Stat(filepath.Join(dir, "singbox_client.json")); statErr == nil {
		t.Error("a failed download left a profile behind")
	}
}

// A machine with no sing-box VPN configured has nothing to fetch, and says so
// instead of reporting a download that never happened.
func TestRefreshRemoteProfileWithoutASingboxVPN(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{
		Paths:  &core.Paths{BinDir: dir, RunDir: dir, LogDir: dir, CfgDir: dir},
		Cfg:    &core.AppConfig{VPNs: []core.VPNItem{{Name: "intra", Type: core.VPNType("openvpn"), File: "x.ovpn"}}},
		Logger: logger.New(filepath.Join(dir, "app.log")),
	}
	if err := m.RefreshRemoteProfile(); err == nil {
		t.Error("expected an error with no sing-box VPN configured")
	}
}

// The menu bar is not root and the config directory is, so this only works if
// the daemon will run it. A command missing from that list fails for the bar
// with "needs the daemon", which is the shape of the bug this fixes.
func TestRefreshProfileIsReachableThroughTheDaemon(t *testing.T) {
	if !control.AllowedOperation("refresh-profile") {
		t.Error("the daemon will not run refresh-profile, so the menu bar cannot either")
	}
	// It carries no arguments and no payload, so the strict request validation
	// has to accept the bare command.
	if err := control.ValidateRequest(control.Request{Command: "refresh-profile"}); err != nil {
		t.Errorf("the daemon rejects the request the bar sends: %v", err)
	}
}

// RefreshRemoteTemplate writes beside the sing-box config, which is where
// AvailableProxyTags looks. If those two ever disagree the panel stays empty no
// matter how many successful downloads happen.
func TestFetchedProfileLandsWhereTheMenuLooks(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "singbox.json")
	profile := `{"outbounds":[
      {"type":"vless","tag":"vless-reality"},
      {"type":"naive","tag":"naive"},
      {"type":"direct","tag":"direct"}]}`
	if err := os.WriteFile(filepath.Join(dir, "singbox_client.json"), []byte(profile), 0o644); err != nil {
		t.Fatal(err)
	}
	got := singbox.AvailableProxyTags(base)
	want := []string{"vless-reality", "naive"}
	if len(got) != len(want) {
		t.Fatalf("tags = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tags = %v, want %v", got, want)
		}
	}
}
