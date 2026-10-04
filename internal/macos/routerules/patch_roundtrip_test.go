package routerules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"jorbiquevpn/internal/macos/core"
)

// slimOverlay mirrors the shipped vpn_cfg.json: only user-editable fields, with
// the full VPN definitions living in the binary (core.BuiltinConfig).
const slimOverlay = `{
  "singbox": { "uuid": "", "enabled_outbounds": ["vless-reality"] },
  "direct_domains": [],
  "direct_bypass_processes": [],
  "direct_bypass_paths": []
}`

func writeSlim(t *testing.T) (*core.Paths, string) {
	t.Helper()
	tmp := t.TempDir()
	cfgDir := filepath.Join(tmp, "cfg")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgFile := filepath.Join(cfgDir, "vpn_cfg.json")
	if err := os.WriteFile(cfgFile, []byte(slimOverlay), 0o644); err != nil {
		t.Fatal(err)
	}
	return &core.Paths{BinDir: tmp, CfgDir: cfgDir, CfgFile: cfgFile}, cfgFile
}

// TestPatchRouteRulesRoundTrip proves the user-edit write-back still works
// against the slim overlay: the patch updates the direct rules in place, and
// ReadConfig merges them back onto the built-in VPN definitions.
func TestPatchRouteRulesRoundTrip(t *testing.T) {
	paths, cfgFile := writeSlim(t)

	err := PatchVpnCfgRouteRules(cfgFile,
		[]string{"direct.com"},          // direct
		[]string{"Code"},                // processes
		[]string{"/Applications/X.app"}) // paths
	if err != nil {
		t.Fatalf("PatchVpnCfgRouteRules: %v", err)
	}

	cfg, err := core.ReadConfig(paths)
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if !reflect.DeepEqual(cfg.DirectDomains, []string{"direct.com"}) {
		t.Errorf("direct_domains: %v", cfg.DirectDomains)
	}
	if !reflect.DeepEqual(cfg.DirectBypassProcesses, []string{"Code"}) {
		t.Errorf("direct_bypass_processes: %v", cfg.DirectBypassProcesses)
	}
	if !reflect.DeepEqual(cfg.DirectBypassPaths, []string{"/Applications/X.app"}) {
		t.Errorf("direct_bypass_paths: %v", cfg.DirectBypassPaths)
	}
	// Count against the built-in itself: a slim user config adds no VPNs.
	if want := len(core.BuiltinConfig().VPNs); len(cfg.VPNs) != want {
		t.Errorf("expected %d VPNs from built-in, got %d", want, len(cfg.VPNs))
	}
}

func TestPatchSingboxRoundTrip(t *testing.T) {
	paths, cfgFile := writeSlim(t)

	uuid := "11111111-2222-3333-4444-555555555555"
	if err := PatchVpnCfgSingboxUUIDAndOutbounds(cfgFile, &uuid, []string{"tuic", "hysteria2"}, []string{"anytls"}, nil); err != nil {
		t.Fatalf("PatchVpnCfgSingboxUUIDAndOutbounds: %v", err)
	}
	cfg, err := core.ReadConfig(paths)
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if cfg.Singbox.UUID != uuid {
		t.Errorf("uuid round-trip: %q", cfg.Singbox.UUID)
	}
	if !reflect.DeepEqual(cfg.Singbox.EnabledOutbounds, []string{"tuic", "hysteria2"}) {
		t.Errorf("outbounds round-trip: %v", cfg.Singbox.EnabledOutbounds)
	}
	// What the user turned off is the half that has to survive: it is what a
	// later build consults, so an outbound the server adds is not silently off.
	if !reflect.DeepEqual(cfg.Singbox.DisabledOutbounds, []string{"anytls"}) {
		t.Errorf("disabled round-trip: %v", cfg.Singbox.DisabledOutbounds)
	}
}

// A configuration written before disabled_outbounds existed has no such key.
// Saving a choice has to add it, or the user's "no" is recorded nowhere.
func TestPatchAddsTheDisabledKeyToAnOlderConfig(t *testing.T) {
	paths, cfgFile := writeSlim(t)

	uuid := "11111111-2222-3333-4444-555555555555"
	if err := PatchVpnCfgSingboxUUIDAndOutbounds(cfgFile, &uuid, []string{"tuic"}, nil, nil); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if err := PatchVpnCfgSingboxUUIDAndOutbounds(cfgFile, nil, []string{"tuic"}, []string{"anytls", "hysteria2"}, nil); err != nil {
		t.Fatalf("second save: %v", err)
	}
	cfg, err := core.ReadConfig(paths)
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if !reflect.DeepEqual(cfg.Singbox.DisabledOutbounds, []string{"anytls", "hysteria2"}) {
		t.Errorf("disabled = %v", cfg.Singbox.DisabledOutbounds)
	}
	if !reflect.DeepEqual(cfg.Singbox.EnabledOutbounds, []string{"tuic"}) {
		t.Errorf("enabled = %v", cfg.Singbox.EnabledOutbounds)
	}
}

func TestPatchDisabledOnlyInitializesSingboxKeys(t *testing.T) {
	paths, cfgFile := writeSlim(t)
	if err := os.WriteFile(cfgFile, []byte(`{"singbox": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, disabled := range [][]string{{"tuic"}, {}} {
		if err := PatchVpnCfgSingboxUUIDAndOutbounds(cfgFile, nil, nil, disabled, nil); err != nil {
			t.Fatal(err)
		}
		cfg, err := core.ReadConfig(paths)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(cfg.Singbox.DisabledOutbounds, disabled) {
			t.Errorf("disabled = %#v, want %#v", cfg.Singbox.DisabledOutbounds, disabled)
		}
	}
}

func TestPatchDisabledOutboundsAfterJSONMarshal(t *testing.T) {
	for _, disabled := range [][]string{{"tuic"}, {}} {
		paths, cfgFile := writeSlim(t)
		original := core.AppConfig{Singbox: core.SingboxSection{
			UUID:             "11111111-2222-3333-4444-555555555555",
			EnabledOutbounds: []string{"tuic", "naive"},
		}}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cfgFile, data, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := PatchVpnCfgSingboxUUIDAndOutbounds(cfgFile, nil, nil, disabled, nil); err != nil {
			t.Fatal(err)
		}
		cfg, err := core.ReadConfig(paths)
		if err != nil {
			t.Fatal(err)
		}
		want := original.Singbox
		want.DisabledOutbounds = disabled
		if !reflect.DeepEqual(cfg.Singbox, want) {
			t.Errorf("patched selection = %#v, want %#v", cfg.Singbox, want)
		}
	}
}

// Regex character classes and literal brackets in application paths are JSON
// string content, not the end of the array being patched.
func TestPatchesIgnoreBracketsInsideJSONStrings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "singbox.json")
	profile := `{
  "dns": {"servers": [], "rules": []},
  "route": {"rules": [
    {"domain_regex": ["^host[}]{2}$"], "outbound": "proxy"},
    {"process_path": ["/Applications/Tool ]Preview.app/Contents/MacOS/tool"], "outbound": "direct"}
  ]}
}`
	if err := os.WriteFile(path, []byte(profile), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := PatchSingboxRouteRules(path, []string{"example.com"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) || !strings.Contains(string(data), `^host[}]{2}$`) {
		t.Fatalf("patch damaged the profile: %s", data)
	}
	old := `{"singbox":{"uuid":"","enabled_outbounds":["node]old"],"disabled_outbounds":[]}}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	node := "node[new]"
	if err := PatchVpnCfgSingboxUUIDAndOutbounds(path, nil, []string{node}, []string{}, &node); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) || strings.Contains(string(data), "node]old") || !strings.Contains(string(data), `"selected_node": "node[new]"`) {
		t.Fatalf("patch damaged the choice: %s", data)
	}
}
