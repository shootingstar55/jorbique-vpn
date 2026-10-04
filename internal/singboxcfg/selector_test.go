package singboxcfg

import (
	"reflect"
	"testing"

	"jorbiquevpn/internal/jsonorder"
)

func parse(t *testing.T, body string) *jsonorder.Map {
	t.Helper()
	cfg := jsonorder.New()
	if err := jsonorder.Unmarshal([]byte(body), cfg); err != nil {
		t.Fatalf("parse: %v", err)
	}
	return cfg
}

// The shape the server serves now: a selector over the urltest and every node.
const selectorProfile = `{"outbounds":[
  {"type":"vless","tag":"vless-reality"},
  {"type":"tuic","tag":"tuic"},
  {"type":"snell","tag":"snell"},
  {"type":"urltest","tag":"auto","outbounds":["vless-reality","tuic","snell"]},
  {"type":"selector","tag":"proxy-selector","outbounds":["auto","vless-reality","tuic","snell"],"default":"auto"},
  {"type":"direct","tag":"direct"}
]}`

func TestSetSelectorDefaultOnlyNamesNodesTheGroupOffers(t *testing.T) {
	cfg := parse(t, selectorProfile)
	SetSelectorDefault(cfg, "tuic")
	if got := selectorField(t, cfg, "default"); got != "tuic" {
		t.Errorf("default = %v, want tuic", got)
	}

	// A node the group does not list must leave the previous default alone: a
	// "default" naming a tag that is not there is a config sing-box refuses to
	// start at all, so a stale choice would cost the user the whole tunnel.
	SetSelectorDefault(cfg, "a-node-the-server-dropped")
	if got := selectorField(t, cfg, "default"); got != "tuic" {
		t.Errorf("default after an unknown node = %v, want it left at tuic", got)
	}

	// No choice stored: the profile's own default stands.
	fresh := parse(t, selectorProfile)
	SetSelectorDefault(fresh, "")
	if got := selectorField(t, fresh, "default"); got != "auto" {
		t.Errorf("default with no choice = %v, want the profile's auto", got)
	}
}

// Pruning a node out of the group has to take the default with it, for the same
// reason: sing-box reads it as a tag like any other.
func TestPruningANodeClearsADefaultNamingIt(t *testing.T) {
	cfg := parse(t, `{"outbounds":[
	  {"type":"tuic","tag":"tuic"},
	  {"type":"selector","tag":"proxy-selector","outbounds":["tuic","snell"],"default":"snell"},
	  {"type":"direct","tag":"direct"}
	]}`)
	PruneDanglingReferences(cfg)

	if got := selectorField(t, cfg, "default"); got != nil {
		t.Errorf("default = %v, want it gone with the node it named", got)
	}
	if got := selectorField(t, cfg, "outbounds"); !reflect.DeepEqual(got, []any{"tuic"}) {
		t.Errorf("group = %v, want only the node still there", got)
	}
}

func selectorField(t *testing.T, cfg *jsonorder.Map, key string) any {
	t.Helper()
	obs, _ := cfg.Get("outbounds")
	for _, o := range obs.([]any) {
		m := o.(*jsonorder.Map)
		if m.GetString("type") != "selector" {
			continue
		}
		v, _ := m.Get(key)
		return v
	}
	t.Fatal("no selector in the config")
	return nil
}

// A resolver can go — its endpoint was switched off — and whatever named it as a
// domain_resolver has to stop naming it. sing-box refuses the config over that
// too, reporting the endpoint rather than the resolver, which is a long way from
// the tunnel the user actually turned off.
func TestPruningAResolverClearsWhatNamedIt(t *testing.T) {
	cfg := parse(t, `{
	  "dns": {"servers": [
	    {"tag": "dns-ovpn-intra", "type": "openvpn", "endpoint": "ovpn-intra"},
	    {"tag": "dns-local", "type": "local"}
	  ]},
	  "endpoints": [],
	  "outbounds": [
	    {"type": "vless", "tag": "vless-reality", "domain_resolver": "dns-ovpn-intra"},
	    {"type": "direct", "tag": "direct"}
	  ],
	  "route": {"default_domain_resolver": "dns-ovpn-intra", "rules": []}
	}`)
	PruneDanglingReferences(cfg)

	// Repointed, not dropped: something with a hostname to look up and no
	// resolver at all is refused too ("missing domain resolver for domain
	// server address"), so the dangling name moves to the resolver that needs
	// no tunnel.
	obs, _ := cfg.Get("outbounds")
	first := obs.([]any)[0].(*jsonorder.Map)
	if got := first.GetString("domain_resolver"); got != "dns-local" {
		t.Errorf("outbound domain_resolver = %q, want dns-local", got)
	}
	route, _ := cfg.Get("route")
	if got := route.(*jsonorder.Map).GetString("default_domain_resolver"); got != "dns-local" {
		t.Errorf("route default_domain_resolver = %q, want dns-local", got)
	}

	// With no resolver left that works without a tunnel there is nothing to
	// point at, and the name is dropped as it always was.
	bare := parse(t, `{
	  "dns": {"servers": [{"tag": "dns-ovpn-intra", "type": "openvpn", "endpoint": "ovpn-intra"}]},
	  "endpoints": [],
	  "outbounds": [{"type": "direct", "tag": "direct", "domain_resolver": "dns-ovpn-intra"}],
	  "route": {"rules": []}
	}`)
	PruneDanglingReferences(bare)
	bareObs, _ := bare.Get("outbounds")
	if bareObs.([]any)[0].(*jsonorder.Map).Has("domain_resolver") {
		t.Error("a dangling resolver was kept with nothing to repoint it at")
	}

	// A resolver that is still there must be left alone.
	cfg = parse(t, `{
	  "dns": {"servers": [{"tag": "dns-local", "type": "local"}]},
	  "outbounds": [{"type": "vless", "tag": "a", "domain_resolver": "dns-local"}],
	  "route": {"default_domain_resolver": "dns-local", "rules": []}
	}`)
	PruneDanglingReferences(cfg)
	obs, _ = cfg.Get("outbounds")
	if got := obs.([]any)[0].(*jsonorder.Map).GetString("domain_resolver"); got != "dns-local" {
		t.Errorf("a live resolver was cleared: %q", got)
	}
}
