package ui

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"jorbiquevpn/internal/macos/core"
)

// strptr returns a pointer to s, for optional config fields.
func strptr(s string) *string { return &s }

func TestAvailableVPNsOnlyOffersSingbox(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "legacy.json")
	if err := os.WriteFile(legacy, []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &core.AppConfig{VPNs: []core.VPNItem{
		{Name: "other", Type: core.VPNType("other"), File: legacy},
		{Name: "old-default", File: legacy},
		{Name: "Jorbique VPN", Type: core.VPNSingbox, File: filepath.Join(dir, "not-downloaded.json")},
		{Name: "blank", Type: core.VPNSingbox},
		{Name: "sentinel", Type: core.VPNSingbox, File: dir},
	}}
	want := []AvailableVPN{{Name: "Jorbique VPN", Type: "singbox"}}
	if got := availableVPNs(cfg, &core.Paths{CfgDir: dir}); !reflect.DeepEqual(got, want) {
		t.Fatalf("available = %#v, want %#v", got, want)
	}
}

func TestStartedVPNsDefaultPath(t *testing.T) {
	stateDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stateDir, "started.list"), []byte("other\n\n  Jorbique VPN \n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	paths := &core.Paths{DataDir: stateDir, StartedListFilename: "started.list"}
	cfg := &core.AppConfig{VPNs: []core.VPNItem{
		{Name: "other", Type: core.VPNType("other")},
		{Name: "Jorbique VPN", Type: core.VPNSingbox},
		{Name: "spare", Type: core.VPNSingbox},
	}}
	// Only the sing-box entries the list names, blanks and padding dropped.
	if got := startedVPNs(cfg, paths); !reflect.DeepEqual(got, []string{"Jorbique VPN"}) {
		t.Errorf("startedVPNs = %v", got)
	}
}

func TestStartedVPNsOverridePath(t *testing.T) {
	dir := t.TempDir()
	custom := filepath.Join(dir, "custom.list")
	if err := os.WriteFile(custom, []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	paths := &core.Paths{LogDir: dir, StartedListFilename: "unused.list"}
	cfg := &core.AppConfig{StartedListPath: strptr(custom), VPNs: []core.VPNItem{{Name: "alpha", Type: core.VPNSingbox}, {Name: "beta", Type: core.VPNSingbox}}}

	got := startedVPNs(cfg, paths)
	want := []string{"alpha", "beta"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("startedVPNs(override) = %v, want %v", got, want)
	}
}

func TestStartedVPNsMissingFile(t *testing.T) {
	paths := &core.Paths{LogDir: t.TempDir(), StartedListFilename: "absent.list"}
	if got := startedVPNs(&core.AppConfig{}, paths); got != nil {
		t.Errorf("startedVPNs(missing) = %v, want nil", got)
	}
}

func TestSingboxHelpers(t *testing.T) {
	cfg := &core.AppConfig{Singbox: core.SingboxSection{
		UUID:             "  abc-123  ",
		EnabledOutbounds: []string{"us", "uk"},
	}}
	if got := singboxUUID(cfg); got != "abc-123" {
		t.Errorf("singboxUUID = %q", got)
	}
	if got := singboxEnabledOutbounds(cfg, []string{"us", "uk"}); !reflect.DeepEqual(got, []string{"us", "uk"}) {
		t.Errorf("singboxEnabledOutbounds = %v", got)
	}
	// No profile and nothing stored -> empty, not nil.
	empty := &core.AppConfig{}
	if got := singboxEnabledOutbounds(empty, nil); got == nil || len(got) != 0 {
		t.Errorf("singboxEnabledOutbounds(empty) = %v, want non-nil empty", got)
	}
}

// The profile can change while the panel is open; Apply must not fail over it.
func TestReconcileSingboxChoice(t *testing.T) {
	shown := []string{"a", "b", "c"}
	for _, tc := range []struct {
		name                           string
		shown, ticked, current, stored []string
		want                           []string
	}{
		{"unchanged", shown, []string{"a", "c"}, shown, nil, []string{"a", "c"}},
		{"ticked tag dropped", shown, []string{"a", "c"}, []string{"a", "b", "d"}, []string{"a", "d"}, []string{"a", "d"}},
		{"unticked tag stays off", shown, []string{"a"}, []string{"a", "b", "c", "d"}, []string{"a", "b", "c", "d"}, []string{"a", "d"}},
		{"unshown refusal stays off", shown, []string{"a"}, []string{"a", "b", "z"}, []string{"a"}, []string{"a"}},
		{"unticked tag re-cased stays off", []string{"Hy2", "vless"}, []string{"vless"}, []string{"hy2", "vless"}, []string{"hy2", "vless"}, []string{"vless"}},
		{"every tick dropped", shown, []string{"c"}, []string{"a", "b"}, nil, []string{"c"}},
		{"profile gone", shown, []string{"a"}, nil, nil, []string{"a"}},
		// Nothing ticked stays that way; the config build is what refuses it.
		{"nothing ticked", shown, nil, []string{"a", "b", "c"}, []string{"a", "b", "c"}, nil},
		// Nothing was shown, so nothing was chosen: the stored choice goes, not
		// the empty line that would switch every protocol off.
		{"nothing shown, profile now", nil, nil, shown, []string{"a"}, []string{"a"}},
		{"nothing shown, still no profile", nil, nil, nil, nil, nil},
	} {
		if got := reconcileSingboxChoice(tc.shown, tc.ticked, tc.current, tc.stored); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestHasSingboxTypeRunning(t *testing.T) {
	available := []AvailableVPN{
		{Name: "intra", Type: "openvpn"},
		{Name: "jorbique", Type: "singbox"},
	}
	if !hasSingboxTypeRunning([]string{"jorbique"}, available) {
		t.Error("expected true when a running VPN is singbox")
	}
	if hasSingboxTypeRunning([]string{"intra"}, available) {
		t.Error("expected false when no running VPN is singbox")
	}
	if hasSingboxTypeRunning(nil, available) {
		t.Error("expected false when nothing running")
	}
}
