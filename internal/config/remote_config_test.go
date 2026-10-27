package config

import (
	"testing"

	"jorbiquevpn/internal/jsonorder"
)

func TestApplyUserUUID_badOutboundType(t *testing.T) {
	cfg := jsonorder.FromMap(map[string]any{
		"outbounds": []any{
			jsonorder.FromMap(map[string]any{"type": "shadowsocks", "tag": "vless-reality"}),
		},
	})
	err := ApplyUserUUID(cfg, "00000000-0000-0000-0000-000000000001", ProxyTagsFromConfig(cfg))
	if err == nil {
		t.Fatal("expected error for unsupported outbound type")
	}
}

func TestApplyUserUUID_setsVlessAndTuic(t *testing.T) {
	cfg := jsonorder.FromMap(map[string]any{
		"outbounds": []any{
			jsonorder.FromMap(map[string]any{"type": "vless", "tag": "vless-reality", "uuid": ""}),
			jsonorder.FromMap(map[string]any{"type": "tuic", "tag": "tuic", "uuid": ""}),
		},
	})
	u := "11111111-1111-1111-1111-111111111111"
	if err := ApplyUserUUID(cfg, u, ProxyTagsFromConfig(cfg)); err != nil {
		t.Fatal(err)
	}
	ob := field[[]any](cfg, "outbounds")
	for _, o := range ob {
		m := o.(*jsonorder.Map)
		if field[any](m, "uuid") != u {
			t.Fatalf("tag %v uuid want %q got %v", field[any](m, "tag"), u, field[any](m, "uuid"))
		}
	}
}

// Everything the profile says is taken as it comes: a section the client has
// never heard of is what a server release looks like, and dropping it used to
// mean waiting for a client release to match.
func TestMergeRemoteWithTunShell_takesTheWholeProfile(t *testing.T) {
	remote := []byte(`{"outbounds":[{"type":"direct","tag":"direct"}],` +
		`"ntp":{"enabled":true,"server":"time.apple.com"},` +
		`"services":[{"type":"api","tag":"local-api"}],` +
		`"log":{"level":"debug"}}`)

	cfg, err := MergeRemoteWithTunShell(remote)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := mustField[any](cfg, "ntp"); !ok {
		t.Error("ntp was dropped")
	}
	if _, ok := mustField[any](cfg, "services"); !ok {
		t.Error("services was dropped")
	}
	// Where the two disagree the profile wins.
	if lvl := field[any](field[*jsonorder.Map](cfg, "log"), "level"); lvl != "debug" {
		t.Errorf("log level = %v, want the profile's", lvl)
	}
	// What the profile leaves out still comes from the template: the TUN inbound,
	// with no stack, so sing-box runs its own.
	inbounds, ok := mustField[[]any](cfg, "inbounds")
	if !ok || len(inbounds) == 0 {
		t.Fatalf("inbounds = %v", field[any](cfg, "inbounds"))
	}
	if stack := field[any](inbounds[0].(*jsonorder.Map), "stack"); stack != nil {
		t.Errorf("inbound stack = %v, want none", stack)
	}
}

// A profile no build can get past must not replace the cached one.
func TestCheckRemoteProfile(t *testing.T) {
	good := `{"outbounds":[{"type":"vless","tag":"a","uuid":""},{"type":"urltest","tag":"auto","outbounds":["a"]}],"route":{"final":"auto"}}`
	if err := checkRemoteProfile([]byte(good)); err != nil {
		t.Errorf("good profile refused: %v", err)
	}
	for _, bad := range []string{
		`{"outbounds":[]}`,
		`{"outbounds":[null]}`,
		`{"outbounds":[{"type":"vless","tag":"a","uuid":""},{"type":"vless","tag":"a","uuid":""}]}`,
	} {
		if err := checkRemoteProfile([]byte(bad)); err == nil {
			t.Errorf("%s was accepted", bad)
		}
	}
}
