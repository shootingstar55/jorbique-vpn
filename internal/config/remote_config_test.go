package config

import (
	"testing"

	"jorbiquevpn/internal/jsonorder"
)

func TestNormalizeHysteria2ServerPorts_hyphenToColon(t *testing.T) {
	cfg := jsonorder.FromMap(map[string]any{
		"outbounds": []any{
			jsonorder.FromMap(map[string]any{
				"type":         "hysteria2",
				"tag":          "hysteria2",
				"server_ports": []any{"2080-3000"},
			}),
		},
	})
	NormalizeHysteria2ServerPorts(cfg)
	ob := field[[]any](cfg, "outbounds")
	m := ob[0].(*jsonorder.Map)
	sp := field[[]any](m, "server_ports")
	if sp[0] != "2080:3000" {
		t.Fatalf("want 2080:3000 got %v", sp[0])
	}
}

func TestNormalizeHysteria2ServerPorts_alreadyColon(t *testing.T) {
	cfg := jsonorder.FromMap(map[string]any{
		"outbounds": []any{
			jsonorder.FromMap(map[string]any{
				"type":         "hysteria2",
				"tag":          "hysteria2",
				"server_ports": []any{"2080:3000"},
			}),
		},
	})
	NormalizeHysteria2ServerPorts(cfg)
	ob := field[[]any](cfg, "outbounds")
	m := ob[0].(*jsonorder.Map)
	sp := field[[]any](m, "server_ports")
	if sp[0] != "2080:3000" {
		t.Fatalf("want 2080:3000 got %v", sp[0])
	}
}

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

	cfg, err := MergeRemoteWithTunShell("system", remote)
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
	// What the profile leaves out still comes from the template: the TUN inbound
	// belongs to the stack the user picked, which the server cannot know.
	inbounds, ok := mustField[[]any](cfg, "inbounds")
	if !ok || len(inbounds) == 0 {
		t.Fatalf("inbounds = %v", field[any](cfg, "inbounds"))
	}
	if stack := field[any](inbounds[0].(*jsonorder.Map), "stack"); stack != "system" {
		t.Errorf("inbound stack = %v, want the requested one", stack)
	}
}
