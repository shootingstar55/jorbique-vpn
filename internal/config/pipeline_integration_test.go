package config

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"jorbiquevpn/internal/jsonorder"
)

// remoteProfileJSON is a minimal stand-in for the profile the update server returns.
// It carries the dns/outbounds/route sections that get merged onto the embedded tun shell.
const remoteProfileJSON = `{
  "dns": {"servers": [{"tag": "dns-remote", "address": "1.1.1.1"}]},
  "outbounds": [
    {"tag": "vless-reality", "type": "vless", "server": "h", "server_port": 443, "uuid": ""},
    {"tag": "tuic", "type": "tuic", "server": "h", "server_port": 443, "uuid": ""},
    {"tag": "hysteria2", "type": "hysteria2", "server": "h", "server_port": 443, "password": ""},
    {"tag": "vless", "type": "urltest", "outbounds": ["vless-reality"]},
    {"tag": "auto", "type": "urltest", "outbounds": ["vless-reality", "tuic", "hysteria2"]},
    {"tag": "direct", "type": "direct"}
  ],
  "route": {"rules": [
    {"protocol": "dns", "action": "hijack-dns"},
    {"outbound": "direct", "action": "route", "source_ip_cidr": ["10.8.0.0/16"]},
    {"outbound": "direct", "action": "route", "ip_is_private": true},
    {"outbound": "direct", "action": "route", "domain_regex": [".*\\.ru$"]}
  ]}
}`

// TestBuildRuntimeConfig_FullPipeline drives the real end-to-end config build:
// embedded tun template + remote profile -> merge -> user UUID -> outbound filtering
// -> ignored processes/paths -> direct domains -> ordered JSON written to disk.
func TestBuildRuntimeConfig_FullPipeline(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	const uuid = "11111111-1111-1111-1111-111111111111"
	enabled := map[string]bool{
		"vless-reality": true, "vless-grpc": true, "tuic": true, "hysteria2": true, "anytls": true,
	}
	outPath, err := BuildRuntimeConfig(
		"system",
		[]byte(remoteProfileJSON), RuntimeOptions{
			UserUUID:            uuid,
			OutboundEnabled:     enabled,
			IgnoredProcessNames: []string{"chrome"},
			IgnoredProcessPaths: []string{"/usr/bin/zoom"},
			SelectedNode:        "tuic",
			UserDirectDomains:   []string{"example.com"},
		},
	)
	if err != nil {
		t.Fatalf("BuildRuntimeConfig: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read runtime config: %v", err)
	}
	var cfg *jsonorder.Map
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("runtime config is not valid JSON: %v\n%s", err, data)
	}

	// The embedded shell sections survive the merge.
	if !cfg.Has("inbounds") || !cfg.Has("log") {
		t.Error("tun shell sections (inbounds/log) missing after merge")
	}

	// User UUID is applied to the vless outbound.
	outbounds := field[[]any](cfg, "outbounds")
	var sawVlessUUID bool
	for _, o := range outbounds {
		m := o.(*jsonorder.Map)
		if field[any](m, "tag") == "vless-reality" && field[any](m, "uuid") == uuid {
			sawVlessUUID = true
		}
	}
	if !sawVlessUUID {
		t.Error("user UUID not applied to vless-reality outbound")
	}

	// Ignored process, process_path, and direct-domain rules were inserted.
	rules := field[[]any](field[*jsonorder.Map](cfg, "route"), "rules")
	var sawProcName, sawProcPath, sawDirectDomain bool
	for _, r := range rules {
		m := r.(*jsonorder.Map)
		if m.Has("process_name") {
			sawProcName = true
		}
		if m.Has("process_path") {
			sawProcPath = true
		}
		if doms, ok := mustField[[]any](m, "domain"); ok {
			for _, d := range doms {
				if d == "example.com" {
					sawDirectDomain = true
				}
			}
		}
	}
	if !sawProcName {
		t.Error("ignored process_name rule not inserted")
	}
	if !sawProcPath {
		t.Error("ignored process_path rule not inserted")
	}
	if !sawDirectDomain {
		t.Error("user direct-domain rule not inserted")
	}

	// The empty credential the profile ships must be filled in.
	if strings.Contains(string(data), `"uuid": ""`) || strings.Contains(string(data), `"password": ""`) {
		t.Error("the empty credential was left in the built config")
	}
}

func TestBuildRuntimeConfig_RejectsAllOutboundsDisabled(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	disabled := map[string]bool{
		"vless-reality": false, "vless-grpc": false, "tuic": false, "hysteria2": false, "anytls": false,
	}
	if _, err := BuildRuntimeConfig("system", []byte(remoteProfileJSON), RuntimeOptions{
		OutboundEnabled: disabled,
	}); err == nil {
		t.Error("disabling every VPN outbound should fail the build")
	}
}

func TestBuildRuntimeConfig_NestedGroupsAndTransportCredentials(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	const profile = `{
	  "outbounds":[
	    {"type":"vless","tag":"a","uuid":"","detour":"transport"},
	    {"type":"tuic","tag":"b","uuid":""},
	    {"type":"new-protocol","tag":"fresh","password":""},
	    {"type":"vless","tag":"off","uuid":""},
	    {"type":"shadowtls","tag":"transport","password":"server-owned"},
	    {"type":"urltest","tag":"first","outbounds":["off"]},
	    {"type":"new-group","tag":"region","outbounds":["b","off"]},
	    {"type":"urltest","tag":"auto","outbounds":["region","a"]},
	    {"type":"selector","tag":"choose","outbounds":["first","auto","direct"],"default":"first"},
	    {"type":"block","tag":"reject"},
	    {"type":"dns","tag":"dns-out"},
	    {"type":"direct","tag":"direct"}
	  ],
	  "route":{"final":"choose"}
	}`
	if got, want := ProxyTagsFromJSON([]byte(profile)), []string{"b", "off", "a", "fresh"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("nested auto priority = %v, want %v", got, want)
	}
	path, err := BuildRuntimeConfig("system", []byte(profile), RuntimeOptions{
		UserUUID:        "user-credential",
		OutboundEnabled: map[string]bool{"off": false},
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	index := map[string]map[string]any{}
	for _, value := range cfg["outbounds"].([]any) {
		outbound := value.(map[string]any)
		index[outbound["tag"].(string)] = outbound
	}
	for tag, want := range map[string][]any{
		"auto": {"region", "a", "fresh"}, "region": {"b"}, "choose": {"auto", "direct"},
	} {
		if got := index[tag]["outbounds"]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s candidates = %v, want %v", tag, got, want)
		}
	}
	if index["off"] != nil || index["first"] != nil || index["choose"]["default"] != "auto" {
		t.Error("disabled leaf, empty group, or selector default survived filtering")
	}
	if index["transport"]["password"] != "server-owned" || index["fresh"]["password"] != "user-credential" || index["a"]["uuid"] != "user-credential" {
		t.Error("proxy and transport credentials were not kept separate")
	}
	if index["reject"] == nil || index["dns-out"] == nil || index["direct"] == nil {
		t.Error("nonproxy plumbing was removed")
	}
	// A route through a group that filtering emptied is not refused: the pruner
	// takes the rule out and points the final at the direct outbound, the same
	// as when every protocol is switched off. The other rule keeps a proxy
	// routed; without it nothing would tunnel and HasTunnel would refuse.
	invalid := strings.Replace(profile, `"final":"choose"`, `"final":"first","rules":[{"outbound":"first"},{"domain":["x"],"outbound":"auto"}]`, 1)
	path, err = BuildRuntimeConfig("system", []byte(invalid), RuntimeOptions{
		UserUUID:        "user-credential",
		OutboundEnabled: map[string]bool{"off": false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if data, err = os.ReadFile(path); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"final": "direct"`) || strings.Contains(string(data), `"outbound": "first"`) {
		t.Errorf("route through an emptied group survived:\n%s", data)
	}
}

// buildOutbounds runs the Windows build on profile and indexes the outbounds
// it wrote by tag.
func buildOutbounds(t *testing.T, profile string, enabled map[string]bool) map[string]map[string]any {
	t.Helper()
	path, err := BuildRuntimeConfig("system", []byte(profile), RuntimeOptions{
		UserUUID:        "user-credential",
		OutboundEnabled: enabled,
	})
	if err != nil {
		t.Fatalf("%s: %v", profile, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	index := map[string]map[string]any{}
	for _, outbound := range cfg.Outbounds {
		index[outbound["tag"].(string)] = outbound
	}
	return index
}

// Turning off the only proxy that detours through a transport leaves it named
// by a group nothing routes to. It is still not a proxy: no user credential.
func TestBuildRuntimeConfig_StrayGroupLeavesTheTransportAlone(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	index := buildOutbounds(t, `{
	  "outbounds":[
	    {"type":"vless","tag":"a","uuid":"","detour":"transport"},
	    {"type":"tuic","tag":"b","uuid":""},
	    {"type":"socks","tag":"transport","password":""},
	    {"type":"selector","tag":"leftover","outbounds":["transport"]},
	    {"type":"urltest","tag":"auto","outbounds":["a","b"]},
	    {"type":"direct","tag":"direct"}
	  ],
	  "route":{"final":"auto"}
	}`, map[string]bool{"a": false})
	if index["transport"] == nil || index["transport"]["password"] != "" {
		t.Errorf("transport = %v, want it kept for the group and given no credential", index["transport"])
	}
	if index["b"]["uuid"] != "user-credential" {
		t.Errorf("proxy b = %v", index["b"])
	}
}

// The Windows build reads the profile's own route, DNS and rule sets: a
// transport one of them names outlives the proxies detouring through it, and a
// transport a routed group offers is the user's to turn off.
func TestBuildRuntimeConfig_ReadsTheProfilesRoute(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	const leaves = `
	    {"type":"vless","tag":"a","uuid":"","detour":"transport"},
	    {"type":"tuic","tag":"b","uuid":""},
	    {"type":"shadowtls","tag":"transport","password":"server-owned"},
	    {"type":"urltest","tag":"auto","outbounds":["a","b"]},
	    {"type":"direct","tag":"direct"}`
	for _, extra := range []string{
		`"dns":{"servers":[{"tag":"remote","type":"udp","server":"1.1.1.1","detour":"transport"}]},"route":{"final":"auto"}`,
		`"route":{"final":"auto","rules":[{"type":"logical","mode":"or","rules":[{"domain":["x"]}],"outbound":"transport"}]}`,
		`"route":{"final":"auto","rule_set":[{"type":"remote","tag":"ads","format":"binary","url":"https://example.com/ads.srs","download_detour":"transport"}]}`,
	} {
		index := buildOutbounds(t, `{"outbounds":[`+leaves+`],`+extra+`}`, map[string]bool{"a": false})
		if index["transport"] == nil || index["transport"]["password"] != "server-owned" {
			t.Errorf("%s: transport = %v, want it kept with its own secret", extra, index["transport"])
		}
	}
	index := buildOutbounds(t, `{"outbounds":[`+leaves+`,{"type":"selector","tag":"proxy","outbounds":["auto","transport"]}],"route":{"final":"proxy"}}`,
		map[string]bool{"a": false, "transport": false})
	if got := index["proxy"]["outbounds"]; !reflect.DeepEqual(got, []any{"auto"}) {
		t.Errorf("proxy = %v, want the transport the user turned off gone", got)
	}
}

// A transport kept only because a resolver, a rule set or a group nothing
// routes to still names it carries no traffic: with every protocol off there is
// nothing to connect through. A route rule that sends traffic straight to it is
// a tunnel.
func TestBuildRuntimeConfig_ATransportNothingRoutesToIsNoTunnel(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	const leaves = `
	    {"type":"vless","tag":"a","uuid":"","detour":"transport"},
	    {"type":"tuic","tag":"b","uuid":""},
	    {"type":"shadowtls","tag":"transport","password":"server-owned"},
	    {"type":"urltest","tag":"auto","outbounds":["a","b"]},
	    {"type":"direct","tag":"direct"}`
	off := map[string]bool{"a": false, "b": false}
	for _, extra := range []string{
		`],"dns":{"servers":[{"tag":"remote","type":"udp","server":"1.1.1.1","detour":"transport"}]},"route":{"final":"auto"}`,
		`],"route":{"final":"auto","rule_set":[{"type":"remote","tag":"ads","format":"binary","url":"https://example.com/ads.srs","download_detour":"transport"}]}`,
		`,{"type":"selector","tag":"leftover","outbounds":["transport"]}],"route":{"final":"auto"}`,
	} {
		_, err := BuildRuntimeConfig("system", []byte(`{"outbounds":[`+leaves+extra+`}`), RuntimeOptions{
			UserUUID:        "user-credential",
			OutboundEnabled: off,
		})
		if err == nil || !strings.Contains(err.Error(), "enable at least one protocol") {
			t.Errorf("%s: err = %v, want nothing to connect through", extra, err)
		}
	}
	index := buildOutbounds(t, `{"outbounds":[`+leaves+`],"route":{"final":"auto","rules":[{"domain":["x"],"outbound":"transport"}]}}`, off)
	if index["transport"] == nil {
		t.Errorf("outbounds = %v, want the routed transport kept", index)
	}
}

// The client runs proxies only: a profile's endpoints never reach the config,
// nor does anything that resolved through one or routed to one — and with
// every protocol off, the endpoints are no tunnel to fall back on.
func TestBuildRuntimeConfig_DropsEndpoints(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	profile := `{
	  "endpoints":[{"type":"openvpn-client","tag":"ovpn-intra"},{"type":"wireguard","tag":"wg"}],
	  "dns":{"servers":[{"tag":"doh","type":"https","server":"1.1.1.1"},{"tag":"intra-dns","type":"openvpn","endpoint":"ovpn-intra"}],
	    "rules":[{"domain_suffix":["lan"],"server":"intra-dns"}],"final":"doh"},
	  "outbounds":[
	    {"type":"vless","tag":"a","uuid":""},
	    {"type":"urltest","tag":"auto","outbounds":["a"]},
	    {"type":"direct","tag":"direct"}
	  ],
	  "route":{"rules":[{"ip_cidr":["10.0.0.0/8"],"outbound":"ovpn-intra"}],"final":"auto"}
	}`
	outPath, err := BuildRuntimeConfig("system", []byte(profile), RuntimeOptions{UserUUID: "user-credential"})
	if err != nil {
		t.Fatalf("BuildRuntimeConfig: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{`"endpoints"`, "ovpn-intra", "intra-dns", "10.0.0.0/8"} {
		if strings.Contains(string(data), gone) {
			t.Errorf("%s survived:\n%s", gone, data)
		}
	}

	_, err = BuildRuntimeConfig("system", []byte(profile), RuntimeOptions{UserUUID: "user-credential", OutboundEnabled: map[string]bool{"a": false}})
	if err == nil || !strings.Contains(err.Error(), "enable at least one protocol") {
		t.Errorf("every protocol off with endpoints in the profile: err = %v, want nothing to connect through", err)
	}
}
