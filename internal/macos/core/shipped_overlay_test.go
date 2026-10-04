package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// shippedOverlay is the vpn_cfg.json the .pkg installs into
// /Library/Application Support/JorbiqueVPN/cfg on a fresh machine.
func shippedOverlay(t *testing.T) SingboxSection {
	t.Helper()
	// From internal/macos/core to internal/macos/cfg.
	path := filepath.Join("..", "cfg", "vpn_cfg.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var cfg struct {
		Singbox SingboxSection `json:"singbox"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return cfg.Singbox
}

// The overlay must not ship a list of outbounds that are on.
//
// It used to name the five tags served when it was written, and that list wins
// over the built-in config on every fresh install. EnabledProxyTags reads a
// non-empty positive list as the user's own choice, so an outbound the server
// added afterwards — `naive`, and whatever follows it — was off on a machine
// where nobody had ever opened the panel, with no way to tell that from a
// deliberate refusal. Empty means "no opinion", which is what a fresh install
// actually has, and reads as everything the profile offers.
func TestShippedOverlayPinsNoOutbounds(t *testing.T) {
	s := shippedOverlay(t)
	if len(s.EnabledOutbounds) != 0 {
		t.Errorf("shipped enabled_outbounds = %v, want empty so a fresh install takes what the profile offers", s.EnabledOutbounds)
	}
	if len(s.DisabledOutbounds) != 0 {
		t.Errorf("shipped disabled_outbounds = %v, want empty so nothing is off out of the box", s.DisabledOutbounds)
	}
}

// The built-in config is the base the overlay merges onto, and it names no
// outbounds either. Both have to stay empty: either one alone reintroduces the
// stale list.
func TestBuiltinConfigPinsNoOutbounds(t *testing.T) {
	c := BuiltinConfig()
	if len(c.Singbox.EnabledOutbounds) != 0 || len(c.Singbox.DisabledOutbounds) != 0 {
		t.Errorf("builtin singbox section names outbounds: %+v", c.Singbox)
	}
}
