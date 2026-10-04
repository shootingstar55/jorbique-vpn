package singboxcfg

import (
	"reflect"
	"testing"

	"jorbiquevpn/internal/jsonorder"
)

// A route final naming something gone is dropped when there is no direct
// outbound to name instead, and a group that never listed members is not
// mistaken for an emptied one.
func TestPruneDropsAFinalWithNoDirectToFallBackTo(t *testing.T) {
	cfg := parse(t, `{
	  "outbounds":[{"type":"selector","tag":"pick"},{"type":"vless","tag":"p"}],
	  "route":{"final":"gone"}
	}`)
	PruneDanglingReferences(cfg)
	if got := outboundTags(cfg); !reflect.DeepEqual(got, []string{"pick", "p"}) {
		t.Errorf("outbounds = %v", got)
	}
	route, _ := getMap(cfg, "route")
	if route.Has("final") {
		t.Errorf("final = %q, want it removed", route.GetString("final"))
	}
}

func TestClearDomainResolver(t *testing.T) {
	servers := map[string]bool{"dns-local": true}
	for _, tc := range []struct {
		name, value, fallback, want string
		kept                        bool
	}{
		{"live resolver stays", "dns-local", "dns-local", "dns-local", true},
		{"gone resolver repointed", "doh", "dns-local", "dns-local", true},
		{"gone resolver dropped", "doh", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := jsonorder.New()
			m.Set("domain_resolver", tc.value)
			clearDomainResolver(m, "domain_resolver", servers, tc.fallback)
			if m.Has("domain_resolver") != tc.kept || m.GetString("domain_resolver") != tc.want {
				t.Errorf("domain_resolver = %q (present %v), want %q", m.GetString("domain_resolver"), m.Has("domain_resolver"), tc.want)
			}
		})
	}
	clearDomainResolver(nil, "domain_resolver", servers, "dns-local") // a missing section is no error
}

func TestHasTunnelFollowsTheRoute(t *testing.T) {
	for _, tc := range []struct {
		name, profile string
		want          bool
	}{
		// No final: sing-box sends unmatched traffic to the first outbound.
		{"first outbound is a proxy", `{"outbounds":[{"type":"vless","tag":"p"},{"type":"direct","tag":"direct"}]}`, true},
		{"first outbound is direct", `{"outbounds":[{"type":"direct","tag":"direct"},{"type":"vless","tag":"p"}]}`, false},
		{"through a group", `{"outbounds":[{"type":"direct","tag":"direct"},{"type":"vless","tag":"p"},
		  {"type":"selector","tag":"sel","outbounds":["direct","p"]}],"route":{"final":"sel"}}`, true},
		{"through a detour", `{"outbounds":[{"type":"direct","tag":"direct"},{"type":"vless","tag":"p"},
		  {"type":"direct","tag":"chained","detour":"p"}],"route":{"final":"chained"}}`, true},
		{"proxy nothing routes to", `{"outbounds":[{"type":"direct","tag":"direct"},{"type":"vless","tag":"p"}],
		  "route":{"final":"direct"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasTunnel(parse(t, tc.profile)); got != tc.want {
				t.Errorf("HasTunnel = %v, want %v", got, tc.want)
			}
		})
	}
}

// Entries that are not objects are carried through untouched rather than
// dropped or tripped over.
func TestNonObjectEntriesAreCarriedThrough(t *testing.T) {
	outbounds := append([]any{"stray"}, parseOutbounds(t, `[{"type":"vless","tag":"p"}]`)...)
	filtered, err := FilterOutbounds(outbounds, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 2 || filtered[0] != "stray" {
		t.Errorf("filtered = %v", filtered)
	}

	root := parse(t, `{"route":{"rules":["junk",{"outbound":"r1"}]}}`)
	if got := RouteEntries(root); !reflect.DeepEqual(got, []string{"r1"}) {
		t.Errorf("entries = %v", got)
	}
}
