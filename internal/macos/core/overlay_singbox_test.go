package core

import (
	"os"
	"path/filepath"
	"testing"
)

// The panels write the whole sing-box section; the overlay used to read three
// fields of it. What the user picked was saved correctly and then read back as
// the default — a node that was ignored.
func TestOverlayCarriesTheWholeSingboxSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vpn_cfg.json")
	if err := os.WriteFile(path, []byte(`{
	  "singbox": {
	    "uuid": "7998568d-7ae4-4f65-a4cb-b20e78af6a87",
	    "enabled_outbounds": ["tuic"],
	    "disabled_outbounds": ["snell"],
	    "selected_node": "tuic"
	  }
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ReadConfig(&Paths{CfgFile: path, CfgDir: dir, BinDir: dir})
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}

	if cfg.Singbox.SelectedNode != "tuic" {
		t.Errorf("selected node = %q, want tuic", cfg.Singbox.SelectedNode)
	}
	if cfg.Singbox.UUID == "" || len(cfg.Singbox.DisabledOutbounds) != 1 {
		t.Errorf("the fields that already worked stopped: %+v", cfg.Singbox)
	}
}

// A file that says nothing about them leaves the built-in defaults alone, which
// is what every config written before these panels existed looks like.
func TestOverlayWithoutTheNewFieldsKeepsTheDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vpn_cfg.json")
	if err := os.WriteFile(path, []byte(`{"singbox": {"uuid": "x"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := ReadConfig(&Paths{CfgFile: path, CfgDir: dir, BinDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Singbox.SelectedNode != "" {
		t.Errorf("an older file invented answers: %+v", cfg.Singbox)
	}
}
