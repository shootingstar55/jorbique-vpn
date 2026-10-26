//go:build darwin

package ui

import (
	"reflect"
	"testing"

	"jorbiquevpn/internal/localapi"
)

func TestSingboxConfigState(t *testing.T) {
	state := singboxConfigState("user-id", []string{"TUIC"}, []string{"vless-reality", "tuic"}, false)
	if state.OutboundEnabled["vless-reality"] || !state.OutboundEnabled["tuic"] {
		t.Fatalf("outbounds = %#v", state.OutboundEnabled)
	}
	// The enabled list is the answer, not a hint: an empty one shows every
	// protocol off. What "no opinion" means is resolved before this —
	// singboxEnabledOutbounds hands back what the profile offers when nothing
	// was ever refused.
	if off := singboxConfigState("", nil, []string{"tuic"}, false); off.OutboundEnabled["tuic"] {
		t.Fatal("an empty selection must show every protocol off")
	}
	if on := singboxConfigState("", []string{"tuic"}, []string{"tuic"}, false); !on.OutboundEnabled["tuic"] {
		t.Fatal("a protocol on the list must show on")
	}
}

func TestSingboxConfigSelection(t *testing.T) {
	uuid := "7998568d-7ae4-4f65-a4cb-b20e78af6a87"
	gotUUID, enabled, err := singboxConfigSelection(localapi.ConfigUpdate{
		UUID:            " " + uuid + " ",
		OutboundEnabled: map[string]bool{"vless-reality": false, "tuic": true},
	}, []string{"vless-reality", "tuic"})
	if err != nil || gotUUID != uuid || !reflect.DeepEqual(enabled, []string{"tuic"}) {
		t.Fatalf("selection = %q %v, %v", gotUUID, enabled, err)
	}

	// The UUID is what every protocol authenticates with: none is refused,
	// whatever is switched on, and so is one that is not a UUID.
	for _, update := range []localapi.ConfigUpdate{
		{OutboundEnabled: map[string]bool{"tuic": true}},
		{OutboundEnabled: map[string]bool{"tuic": false}},
		{UUID: "not-a-uuid"},
	} {
		if _, _, err := singboxConfigSelection(update, []string{"tuic"}); err == nil {
			t.Errorf("selection %+v was accepted", update)
		}
	}
}
