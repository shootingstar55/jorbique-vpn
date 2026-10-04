package vpnmgr

import (
	"strings"
	"testing"

	"jorbiquevpn/internal/macos/core"
)

func TestStartRejectsStandaloneVPNBeforeTouchingTheSystem(t *testing.T) {
	for _, kind := range []core.VPNType{"openvpn", "", "unknown"} {
		m := &Manager{}
		err := m.startOne(core.VPNItem{Name: "legacy", Type: kind, File: "legacy.ovpn"})
		if err == nil || !strings.Contains(err.Error(), "unsupported VPN type") {
			t.Errorf("start %q = %v, want unsupported VPN type", kind, err)
		}
	}
}

func TestResolveCoreWithoutGeneratedConfig(t *testing.T) {
	v := core.VPNItem{Name: "Jorbique VPN", Type: core.VPNSingbox, File: "not-generated-yet.json"}
	m := testManager(t, v)
	for _, target := range []string{"all", v.Name} {
		if got := m.resolveTargets(target); len(got) != 1 || got[0].Name != v.Name {
			t.Errorf("resolve %q = %v, want the core that will generate its config", target, got)
		}
	}
}

func TestStartedListDropsRetiredProfiles(t *testing.T) {
	v := core.VPNItem{Name: "Jorbique VPN", Type: core.VPNSingbox}
	m := testManager(t, v)
	m.saveStartedList([]string{"intra", "jorbique-prod", v.Name})
	if got := m.loadStartedList(); len(got) != 1 || got[0] != v.Name {
		t.Fatalf("started list = %v, want only the configured core", got)
	}
}
