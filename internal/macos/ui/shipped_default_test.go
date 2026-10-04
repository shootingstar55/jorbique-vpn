package ui

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/macos/singbox"
)

// The panel and the config builder read a stored choice through different
// functions, and they have to reach the same answer. A machine still carrying
// the packaged list has made no choice: with a profile that is everything it
// offers, and without one it is no opinion — never the stale five, which would
// show a tag the server added as off while the tunnel was built with it on.
func TestPanelReadsAnUnchosenShippedDefaultAsAllAvailable(t *testing.T) {
	shipped := []string{"vless-reality", "vless-grpc", "tuic", "hysteria2", "anytls"}
	cfg := &core.AppConfig{Singbox: core.SingboxSection{EnabledOutbounds: shipped}}
	if got := singboxEnabledOutbounds(cfg, nil); len(got) != 0 {
		t.Errorf("selection without a profile = %v, want empty", got)
	}
	available := append(append([]string{}, shipped...), "naive")
	if got := singboxEnabledOutbounds(cfg, available); !reflect.DeepEqual(got, available) {
		t.Errorf("panel initial selection = %v, want %v", got, available)
	}
	built := singbox.EnabledProxyTags(available, shipped, nil)
	if !reflect.DeepEqual(built, available) {
		t.Errorf("built with %v, want everything %v — the two paths disagree", built, available)
	}
}

// A choice someone made is shown back to them unchanged.
func TestPanelKeepsAHandPickedSelection(t *testing.T) {
	chosen := []string{"tuic", "hysteria2"}
	cfg := &core.AppConfig{Singbox: core.SingboxSection{EnabledOutbounds: chosen}}

	if got := singboxEnabledOutbounds(cfg, []string{"tuic", "hysteria2", "naive"}); !reflect.DeepEqual(got, chosen) {
		t.Errorf("panel initial selection = %v, want %v", got, chosen)
	}
}

// The disabled list, rather than the older positive list, decides the selection.
func TestPanelKeepsTheFiveWhenARefusalWasRecorded(t *testing.T) {
	shipped := []string{"vless-reality", "vless-grpc", "tuic", "hysteria2", "anytls"}
	cfg := &core.AppConfig{Singbox: core.SingboxSection{
		EnabledOutbounds:  shipped,
		DisabledOutbounds: []string{"naive"},
	}}

	available := append(append([]string{}, shipped...), "naive")
	if got := singboxEnabledOutbounds(cfg, available); !reflect.DeepEqual(got, shipped) {
		t.Errorf("panel initial selection = %v, want the user's own %v", got, shipped)
	}
}

// Every protocol refused. Stored, that
// is an empty positive list beside a full negative one — which the panel used
// to read as "nothing was ever chosen" and show as all-on. The stack picker
// sends this same list to the daemon, which recomputes the refusals from it, so
// the misreading did not just look wrong: picking a stack turned every protocol
// back on.
func TestPanelReadsEveryProtocolRefusedAsRefused(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "singbox_client.json")
	if err := os.WriteFile(profile, []byte(`{"outbounds":[
	  {"type":"vless","tag":"vless-reality"},
	  {"type":"tuic","tag":"tuic"},
	  {"type":"direct","tag":"direct"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &core.AppConfig{
		VPNs: []core.VPNItem{{Type: core.VPNSingbox, File: filepath.Join(dir, "singbox.json")}},
		Singbox: core.SingboxSection{
			EnabledOutbounds:  []string{},
			DisabledOutbounds: []string{"vless-reality", "tuic"},
		},
	}
	if got := singboxAvailableOutbounds(cfg); len(got) != 2 {
		t.Fatalf("the profile should offer two protocols, got %v", got)
	}
	if got := singboxEnabledOutbounds(cfg, singboxAvailableOutbounds(cfg)); len(got) != 0 {
		t.Errorf("panel selection = %v, want none — all of them were refused", got)
	}

	// One of them back on, and that is what shows.
	cfg.Singbox.DisabledOutbounds = []string{"vless-reality"}
	if got := singboxEnabledOutbounds(cfg, singboxAvailableOutbounds(cfg)); !reflect.DeepEqual(got, []string{"tuic"}) {
		t.Errorf("panel selection = %v, want [tuic]", got)
	}
}

func TestPanelReconcilesRecordedChoiceAgainstCurrentProfile(t *testing.T) {
	for _, tt := range []struct {
		name                               string
		available, enabled, disabled, want []string
	}{
		{"new outbound stays on", []string{"tuic", "naive"}, []string{"tuic"}, []string{}, []string{"tuic", "naive"}},
		{"removed outbound disappears", []string{"tuic"}, []string{"tuic", "removed"}, []string{}, []string{"tuic"}},
		{"explicit all off stays off", []string{"tuic"}, []string{"removed"}, []string{"tuic"}, []string{}},
		// Nothing to reconcile against yet, so the stored answer stands.
		{"no profile", nil, []string{"tuic"}, []string{}, []string{"tuic"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &core.AppConfig{Singbox: core.SingboxSection{EnabledOutbounds: tt.enabled, DisabledOutbounds: tt.disabled}}
			if got := singboxEnabledOutbounds(cfg, tt.available); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("panel selection = %v, want %v", got, tt.want)
			}
		})
	}
}
