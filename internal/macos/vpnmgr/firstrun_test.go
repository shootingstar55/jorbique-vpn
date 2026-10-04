package vpnmgr

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/macos/logger"
)

// newFirstRunManager is a machine as the .pkg leaves it: a config file with an
// empty sing-box section and no downloaded profile beside it.
func newFirstRunManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "vpn_cfg.json")
	if err := os.WriteFile(cfgFile, []byte(`{
  "singbox": {"uuid": "", "enabled_outbounds": [], "disabled_outbounds": []},
  "vpns": []
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	v := core.VPNItem{Name: "zz-firstrun-vpn", Type: core.VPNSingbox, File: filepath.Join(dir, "singbox.json")}
	return &Manager{
		Paths:  &core.Paths{BinDir: dir, RunDir: dir, LogDir: dir, CfgDir: dir, CfgFile: cfgFile, SingboxFallbackFile: "singbox.json"},
		Cfg:    &core.AppConfig{VPNs: []core.VPNItem{v}},
		Logger: logger.New(filepath.Join(dir, "app.log")),
	}
}

// A fresh machine has no profile, so the panel has no boxes to tick and applies
// an empty selection. Refusing that is a deadlock: the UUID is what a config
// build needs, a config build is what downloads the profile, and the profile is
// what the boxes come from — so the user can never save a UUID and the client
// can never start.
//
// Empty means "nothing was on offer" here, not "everything was unticked", and
// that reads as no opinion: whatever the profile turns out to list.
func TestSetSingboxConfigAcceptsNoSelectionBeforeAProfileExists(t *testing.T) {
	m := newFirstRunManager(t)
	if got := m.availableSingboxOutbounds(); len(got) != 0 {
		t.Fatalf("a fresh machine should offer nothing, got %v", got)
	}

	const uuid = "7998568d-7ae4-4f65-a4cb-b20e78af6a87"
	if err := m.SetSingboxConfig([]string{uuid, ""}); err != nil {
		t.Fatalf("a first run cannot save its UUID: %v", err)
	}

	saved, rerr := os.ReadFile(m.Paths.CfgFile)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(saved), uuid) {
		t.Errorf("the UUID was not stored:\n%s", saved)
	}
}

// Unticking every protocol is stored as chosen. Refusing it here reached
// further than the box it guarded — the stack panel sends the stored selection
// along with the stack, so a client in that state could not change its stack at
// all. A config with no tunnel left is refused by the config build instead.
func TestSetSingboxConfigAcceptsUntickingEveryProtocol(t *testing.T) {
	m := newFirstRunManager(t)
	// A downloaded profile beside the sing-box config gives the panel something
	// to have offered.
	profile := filepath.Join(filepath.Dir(m.Cfg.VPNs[0].File), "singbox_client.json")
	if err := os.WriteFile(profile, []byte(`{"outbounds":[
      {"type":"vless","tag":"vless-reality"},
      {"type":"direct","tag":"direct"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := m.availableSingboxOutbounds(); len(got) == 0 {
		t.Fatalf("the profile should offer something, got %v", got)
	}

	// No account on this machine, so nothing is rebuilt and the save is all
	// there is to check — which is the point: the choice is recorded, not
	// turned away.
	if err := m.SetSingboxConfig([]string{"", ""}); err != nil {
		t.Fatalf("unticking every protocol: %v", err)
	}
	saved, rerr := os.ReadFile(m.Paths.CfgFile)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(saved), `"disabled_outbounds": [`) ||
		!strings.Contains(string(saved), `"vless-reality"`) {
		t.Errorf("the refusal was not recorded:\n%s", saved)
	}
}

// The stack is a local setting — which TUN sing-box builds — and has nothing to
// do with the account. The panel sends whatever UUID is stored, so on a machine
// where none is it sent an empty line, and the save was refused outright with
// "UUID required": the stack could not be changed before an account had ever
// been entered.
func TestTheStackCanBeChangedBeforeAUUIDExists(t *testing.T) {
	m := newFirstRunManager(t)

	// What the stack panel sends: no account, no selection, no node — and the
	// stack on the fourth line.
	if err := m.SetSingboxConfig([]string{"", "", "", "gvisor"}); err != nil {
		t.Fatalf("changing the stack without a UUID: %v", err)
	}

	saved, err := os.ReadFile(m.Paths.CfgFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), `"stack": "gvisor"`) {
		t.Errorf("the stack was not stored:\n%s", saved)
	}
}

// The daemon outlives every save the menu bar makes. Healing with the config it
// started with rebuilt a profile the user had since changed — proxies back on,
// no uuid for them — and the restart died after the core was already stopped.
func TestAutostartCheckPicksUpTheConfigSavedSinceTheDaemonStarted(t *testing.T) {
	m := newFirstRunManager(t)
	if err := os.WriteFile(m.Paths.CfgFile, []byte(`{"singbox": {"disabled_outbounds": ["vless-reality"]}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	m.reloadConfigIfChanged()
	if got := m.Cfg.Singbox.DisabledOutbounds; len(got) != 1 || got[0] != "vless-reality" {
		t.Fatalf("disabled outbounds after a save = %v, want [vless-reality]", got)
	}
	loaded := m.Cfg
	m.reloadConfigIfChanged()
	if m.Cfg != loaded {
		t.Error("an unchanged vpn_cfg.json was read again")
	}
}

// With a profile downloaded, the choice is checked against it and saved as it
// spells the tags; an empty line is every protocol off, and no line at all is
// no choice. Without one a choice is stored as given, the refusals stay as
// they were, and an empty line is no choice at all: the panel had nothing to
// tick.
func TestSetSingboxConfigValidatesAgainstCachedProfile(t *testing.T) {
	for _, tt := range []struct {
		name, input, wantError string
		cache                  bool
		enabled, disabled      []string
	}{
		{name: "empty is every protocol off", cache: true, enabled: []string{}, disabled: []string{"tuic", "naive"}},
		{name: "no outbounds line keeps stored choice", input: "-", cache: true, enabled: []string{"tuic"}, disabled: []string{"naive"}},
		{name: "empty keeps stored choice without cache", enabled: []string{"tuic"}, disabled: []string{"naive"}},
		{name: "canonical tags and duplicates", input: "TuIc;tuic", cache: true, enabled: []string{"tuic"}, disabled: []string{"naive"}},
		{name: "unknown tag", input: "typo", cache: true, wantError: "Unknown outbound"},
		{name: "mixed valid and unknown", input: "tuic,typo", cache: true, wantError: "Unknown outbound"},
		{name: "uncached explicit choice", input: "tuic", enabled: []string{"tuic"}, disabled: []string{"naive"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := newFirstRunManager(t)
			if err := os.WriteFile(m.Paths.CfgFile, []byte(`{"singbox":{"uuid":"old","enabled_outbounds":["tuic"],"disabled_outbounds":["naive"]},"vpns":[]}`), 0o644); err != nil {
				t.Fatal(err)
			}
			if tt.cache {
				profile := filepath.Join(filepath.Dir(m.Cfg.VPNs[0].File), "singbox_client.json")
				if err := os.WriteFile(profile, []byte(`{"outbounds":[
                    {"type":"tuic","tag":"tuic"},
                    {"type":"naive","tag":"naive"},
                    {"type":"direct","tag":"direct"}]}`), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(m.Paths.CfgFile)
			if err != nil {
				t.Fatal(err)
			}
			lines := []string{"7998568d-7ae4-4f65-a4cb-b20e78af6a87", tt.input}
			if tt.input == "-" {
				lines = lines[:1]
			}
			err = m.SetSingboxConfig(lines)
			saved, readErr := os.ReadFile(m.Paths.CfgFile)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Errorf("error = %v, want %q", err, tt.wantError)
				}
				if string(saved) != string(before) {
					t.Errorf("invalid selection changed the saved config:\n%s", saved)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var cfg core.AppConfig
			if err := json.Unmarshal(saved, &cfg); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(cfg.Singbox.EnabledOutbounds, tt.enabled) || !reflect.DeepEqual(cfg.Singbox.DisabledOutbounds, tt.disabled) {
				t.Errorf("saved choice = %#v, want enabled=%#v disabled=%#v", cfg.Singbox, tt.enabled, tt.disabled)
			}
		})
	}
}
