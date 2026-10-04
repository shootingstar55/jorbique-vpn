package core

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func vpnByName(cfg *AppConfig, name string) *VPNItem {
	for i := range cfg.VPNs {
		if cfg.VPNs[i].Name == name {
			return &cfg.VPNs[i]
		}
	}
	return nil
}

func TestBuiltinConfigHasStructuralData(t *testing.T) {
	cfg := BuiltinConfig()
	if len(cfg.VPNs) != 1 || cfg.VPNs[0].Type != VPNSingbox {
		t.Fatalf("only the sing-box core may be launched: %+v", cfg.VPNs)
	}
	if !reflect.DeepEqual(cfg.RequiredTools, []string{"sing-box"}) {
		t.Fatalf("unexpected tool dependency: %v", cfg.RequiredTools)
	}
	if cfg.Api == nil || len(cfg.Api.IPUrls) < 10 {
		t.Error("api ip_urls fallback list must be compiled in")
	}
	if cfg.Autostart == nil || cfg.Autostart.HeartbeatIntervalSeconds == nil {
		t.Error("autostart heartbeat must be compiled in")
	}
}

func TestApplyOverlayMergesUserFields(t *testing.T) {
	cfg := BuiltinConfig()
	overlay := []byte(`{
		"singbox": { "uuid": "abc-123", "enabled_outbounds": ["tuic"] },
		"direct_domains": ["example.com"]
	}`)
	applyOverlay(cfg, overlay)

	if cfg.Singbox.UUID != "abc-123" {
		t.Errorf("uuid not merged: %q", cfg.Singbox.UUID)
	}
	if !reflect.DeepEqual(cfg.Singbox.EnabledOutbounds, []string{"tuic"}) {
		t.Errorf("outbounds not merged: %v", cfg.Singbox.EnabledOutbounds)
	}
	if !reflect.DeepEqual(cfg.DirectDomains, []string{"example.com"}) {
		t.Errorf("direct_domains not merged: %v", cfg.DirectDomains)
	}
}

func TestApplyOverlayIgnoresMalformed(t *testing.T) {
	cfg := BuiltinConfig()
	applyOverlay(cfg, []byte("not json"))
	if len(cfg.VPNs) != len(BuiltinConfig().VPNs) {
		t.Error("malformed overlay must leave built-in config intact")
	}
}

func TestReadConfigMergesOverlayFile(t *testing.T) {
	tmp := t.TempDir()
	cfgDir := filepath.Join(tmp, "cfg")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgFile := filepath.Join(cfgDir, "vpn_cfg.json")
	if err := os.WriteFile(cfgFile, []byte(`{
		"vpns": [ { "name": "extra", "type": "singbox", "file": "cfg/extra.json" } ],
		"direct_bypass_processes": ["Code"]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	paths := &Paths{BinDir: tmp, CfgDir: cfgDir, CfgFile: cfgFile}

	cfg, err := ReadConfig(paths)
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if len(cfg.VPNs) != len(BuiltinConfig().VPNs) {
		t.Fatalf("overlay must not add or remove VPNs, got %d", len(cfg.VPNs))
	}
	// File paths resolved to absolute against BinDir.
	if v := cfg.VPNs[0]; !filepath.IsAbs(v.File) {
		t.Errorf("core file not resolved to absolute: %q", v.File)
	}
	if !reflect.DeepEqual(cfg.DirectBypassProcesses, []string{"Code"}) {
		t.Errorf("direct_bypass_processes not merged: %v", cfg.DirectBypassProcesses)
	}
}

func TestReadConfigMissingFileUsesBuiltin(t *testing.T) {
	tmp := t.TempDir()
	paths := &Paths{BinDir: tmp, CfgDir: filepath.Join(tmp, "cfg"), CfgFile: filepath.Join(tmp, "cfg", "absent.json")}
	cfg, err := ReadConfig(paths)
	if err != nil {
		t.Fatalf("ReadConfig(missing) should not error: %v", err)
	}
	if len(cfg.VPNs) != len(BuiltinConfig().VPNs) {
		t.Errorf("missing overlay should yield built-in VPNs, got %d", len(cfg.VPNs))
	}
}
