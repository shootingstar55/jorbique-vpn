package singboxcfg

import (
	"encoding/json"
	"strings"
	"testing"

	"jorbiquevpn/internal/jsonorder"
)

// Taking an outbound out is never one deletion: whatever resolved through it
// goes, the rules that named that resolver go with the resolver, a "final"
// pointing at any of them stops pointing, and a group outbound stops listing it
// — all of it, in one pass, without the caller naming the relationships. A
// resolver bound to an endpoint goes too: the client builds without endpoints.
func TestPruneDanglingReferencesFollowsTheChain(t *testing.T) {
	cfg := jsonorder.New()
	if err := jsonorder.Unmarshal([]byte(`{
	  "outbounds":[
	    {"type":"vless","tag":"vless"},
	    {"type":"urltest","tag":"auto","outbounds":["vless","gone"]},
	    {"type":"direct","tag":"direct"}
	  ],
	  "dns":{
	    "servers":[
	      {"tag":"doh","type":"https","server":"1.1.1.1","detour":"auto"},
	      {"tag":"dns-endpoint","type":"openvpn","endpoint":"ovpn"},
	      {"tag":"dns-chained","type":"udp","server":"10.0.0.1","detour":"gone"}
	    ],
	    "rules":[
	      {"domain":["a.lan"],"server":"dns-endpoint"},
	      {"domain":["b.lan"],"server":"dns-chained"},
	      {"domain":["c.example"],"server":"doh"}
	    ],
	    "final":"dns-chained"
	  },
	  "route":{"rules":[
	    {"action":"sniff"},
	    {"ip_cidr":["10.1.0.0/16"],"outbound":"gone"},
	    {"ip_cidr":["10.2.0.0/16"],"outbound":"ovpn"},
	    {"ip_is_private":true,"outbound":"direct"}
	  ],"final":"auto"}
	}`), cfg); err != nil {
		t.Fatal(err)
	}

	PruneDanglingReferences(cfg)

	rendered, _ := json.Marshal(cfg)
	for _, gone := range []string{`"gone"`, "dns-endpoint", "dns-chained", "a.lan", "b.lan", "ovpn"} {
		if strings.Contains(string(rendered), gone) {
			t.Errorf("%s survived:\n%s", gone, rendered)
		}
	}
	// Everything that stands on its own is untouched.
	for _, want := range []string{`"tag":"doh"`, "c.example", `"action":"sniff"`, `"outbound":"direct"`, `"final":"auto"`} {
		if !strings.Contains(string(rendered), want) {
			t.Errorf("%s was removed:\n%s", want, rendered)
		}
	}
	// A final that pointed at a removed resolver stops pointing rather than
	// naming something sing-box will not find.
	if dns := field[*jsonorder.Map](cfg, "dns"); dns.Has("final") {
		t.Error("dns.final still names the removed resolver")
	}
	// And the group outbound stops offering what is not there.
	auto := field[[]any](cfg, "outbounds")[1].(*jsonorder.Map)
	if list := field[[]any](auto, "outbounds"); len(list) != 1 || list[0] != "vless" {
		t.Errorf("urltest outbounds = %v", list)
	}
}

// A logical rule whose inner rules all named something gone matches everything
// if it is left behind, which is the opposite of what it was written to do.
func TestPruneDanglingReferencesDropsAnEmptiedLogicalRule(t *testing.T) {
	cfg := jsonorder.New()
	if err := jsonorder.Unmarshal([]byte(`{
	  "outbounds":[{"type":"direct","tag":"direct"}],
	  "route":{"rules":[
	    {"type":"logical","mode":"or","rules":[{"outbound":"gone"}],"action":"route","outbound":"direct"},
	    {"type":"logical","mode":"or","rules":[{"outbound":"direct"}],"action":"route","outbound":"direct"}
	  ]}
	}`), cfg); err != nil {
		t.Fatal(err)
	}

	PruneDanglingReferences(cfg)

	rules := field[[]any](field[*jsonorder.Map](cfg, "route"), "rules")
	if len(rules) != 1 {
		t.Fatalf("route rules = %d, want the emptied logical rule dropped", len(rules))
	}
}

// A rule set downloading through a group the build emptied names nothing, and
// sing-box refuses the config over it. It downloads through the default outbound instead; a detour that is
// still there is left alone.
func TestPruneDanglingReferencesDropsAGoneDownloadDetour(t *testing.T) {
	cfg := jsonorder.New()
	if err := jsonorder.Unmarshal([]byte(`{
	  "outbounds":[
	    {"type":"urltest","tag":"auto","outbounds":["gone"]},
	    {"type":"direct","tag":"direct"}
	  ],
	  "route":{"final":"auto","rule_set":[
	    {"type":"remote","tag":"ads","url":"https://example.com/ads.srs","download_detour":"auto"},
	    {"type":"remote","tag":"geo","url":"https://example.com/geo.srs","download_detour":"direct"}
	  ]}
	}`), cfg); err != nil {
		t.Fatal(err)
	}

	PruneDanglingReferences(cfg)

	sets := field[[]any](field[*jsonorder.Map](cfg, "route"), "rule_set")
	if len(sets) != 2 {
		t.Fatalf("rule sets = %v, want both kept", sets)
	}
	if ads := sets[0].(*jsonorder.Map); ads.Has("download_detour") {
		t.Errorf("ads still downloads through %v", field[any](ads, "download_detour"))
	}
	if got := field[string](sets[1].(*jsonorder.Map), "download_detour"); got != "direct" {
		t.Errorf("geo download_detour = %q, want direct kept", got)
	}
}

// field reads a typed value out of an ordered object, the way a plain map index
// would. The tests below poke at configs constantly.
func field[T any](m *jsonorder.Map, key string) T {
	var zero T
	if m == nil {
		return zero
	}
	v, ok := m.Get(key)
	if !ok {
		return zero
	}
	typed, _ := v.(T)
	return typed
}

// outboundTags lists the outbound tags a config still has.
func outboundTags(cfg *jsonorder.Map) []string {
	var out []string
	raw, _ := cfg.Get("outbounds")
	list, _ := raw.([]any)
	for _, o := range list {
		if m, ok := o.(*jsonorder.Map); ok {
			if tag := m.GetString("tag"); tag != "" {
				out = append(out, tag)
			}
		}
	}
	return out
}

// dnsServer finds one dns server by tag, for the assertions above.
func dnsServer(t *testing.T, cfg *jsonorder.Map, tag string) *jsonorder.Map {
	t.Helper()
	dns, _ := cfg.Get("dns")
	dnsMap, _ := dns.(*jsonorder.Map)
	raw, _ := dnsMap.Get("servers")
	list, _ := raw.([]any)
	for _, s := range list {
		if m, ok := s.(*jsonorder.Map); ok && m.GetString("tag") == tag {
			return m
		}
	}
	t.Fatalf("no dns server tagged %q", tag)
	return nil
}
