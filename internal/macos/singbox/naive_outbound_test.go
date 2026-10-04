package singbox

import (
	"jorbiquevpn/internal/macos/core"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The staging profile's outbounds, naive included.
const naiveProfile = `{"outbounds":[
  {"type":"vless","tag":"vless-reality","uuid":""},
  {"type":"anytls","tag":"anytls","password":""},
  {"type":"naive","tag":"naive","server":"nvp.example","username":"","password":""},
  {"type":"urltest","tag":"auto","outbounds":["vless-reality","anytls","naive"]},
  {"type":"direct","tag":"direct"}
]}`

// naive is a proxy like any other — it carries traffic and takes the user's
// credential — but not a protocol to toggle: it is a mode of its own, so the
// panel's checkbox list leaves it out.
func TestNaiveIsAProxyButNotAToggle(t *testing.T) {
	got := ProxyTagsFromOutbounds(parseOutbounds(t, naiveProfile), nil)
	if want := []string{"vless-reality", "anytls", "naive"}; !reflect.DeepEqual(got, want) {
		t.Errorf("proxy tags = %v, want %v", got, want)
	}
	base := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(remoteTemplateCachePath(base), []byte(naiveProfile), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := AvailableProxyTags(base), []string{"vless-reality", "anytls"}; !reflect.DeepEqual(got, want) {
		t.Errorf("AvailableProxyTags = %v, want %v", got, want)
	}
}

// The protocols mode never starts naive: in the failover group it would run
// beside every other protocol, and it is the heaviest of them.
func TestProtocolsModeLeavesNaiveOut(t *testing.T) {
	base := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(remoteTemplateCachePath(base), []byte(naiveProfile), 0o644); err != nil {
		t.Fatal(err)
	}
	// A 1ns timeout stands in for an unreachable server, so the cached profile
	// above is what gets built.
	root, err := BuildConfigFromRemoteTemplate(core.SingboxSection{UUID: "11111111-1111-1111-1111-111111111111"}, base, time.Nanosecond, nil)
	if err != nil {
		t.Fatalf("BuildConfigFromRemoteTemplate: %v", err)
	}
	out, err := root.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), `"tag":"naive"`) {
		t.Errorf("the protocols mode kept naive:\n%s", out)
	}
	if deps := urltestDeps(t, root); !reflect.DeepEqual(deps, []string{"vless-reality", "anytls"}) {
		t.Errorf("urltest outbounds = %v, want the protocols without naive", deps)
	}
}

// naiveproxy authenticates with a username *and* a password. Every empty field
// of an outbound is a placeholder for the credential, because naming the
// fields meant a client release before a new protocol could authenticate, so
// with both left empty both get the UUID.
func TestNaiveModeTakesTheUUIDInItsEmptyFields(t *testing.T) {
	base := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(remoteTemplateCachePath(base), []byte(naiveProfile), 0o644); err != nil {
		t.Fatal(err)
	}

	const uuid = "11111111-1111-1111-1111-111111111111"
	root, err := BuildConfigFromRemoteTemplate(core.SingboxSection{UUID: uuid, NaiveMode: true}, base, time.Nanosecond, nil)
	if err != nil {
		t.Fatalf("BuildConfigFromRemoteTemplate: %v", err)
	}
	out, err := root.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	built := string(out)

	if !strings.Contains(built, `"tag":"naive"`) {
		t.Fatalf("the naive outbound was dropped from the built config:\n%s", built)
	}
	if !strings.Contains(built, `"username":"`+uuid+`"`) || !strings.Contains(built, `"password":"`+uuid+`"`) {
		t.Errorf("naive did not get the user UUID in its empty fields:\n%s", built)
	}
	// And the failover list is naive alone. Checked on the parsed urltest
	// rather than on the text, where the tag on the outbound itself would
	// match and prove nothing.
	if deps := urltestDeps(t, root); !reflect.DeepEqual(deps, []string{"naive"}) {
		t.Errorf("urltest outbounds = %v, want naive alone", deps)
	}
}

// urltestDeps is the outbounds list of the built config's urltest.
func urltestDeps(t *testing.T, root *OrderedMap) []string {
	t.Helper()
	raw, ok := root.Get("outbounds")
	if !ok {
		t.Fatal("built config has no outbounds")
	}
	arr, _ := raw.([]any)
	for _, ob := range arr {
		om, ok := ob.(*OrderedMap)
		if !ok {
			continue
		}
		if typ, _ := om.Get("type"); typ != "urltest" {
			continue
		}
		list, _ := om.Get("outbounds")
		items, _ := list.([]any)
		var out []string
		for _, v := range items {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	t.Fatal("built config has no urltest")
	return nil
}

// The NaiveProxy mode builds from the same profile as the protocols, keeps naive
// alone, and the protocol toggles have no say in it.
func TestNaiveModeBuildsFromTheMainProfile(t *testing.T) {
	base := filepath.Join(t.TempDir(), "config.json")
	profile := `{"outbounds":[{"type":"vless","tag":"vless-reality","uuid":""},
	  {"type":"naive","tag":"naive","server":"192.0.2.1","server_port":443,
	  "username":"","password":"naivepassword","insecure_concurrency":2,"udp_over_tcp":true},
	  {"type":"urltest","tag":"auto","outbounds":["vless-reality"]},{"type":"direct","tag":"direct"}],
	  "route":{"final":"auto"}}`
	if err := os.WriteFile(remoteTemplateCachePath(base), []byte(profile), 0o644); err != nil {
		t.Fatal(err)
	}

	const uuid = "11111111-1111-1111-1111-111111111111"
	settings := core.SingboxSection{UUID: uuid, DisabledOutbounds: []string{"naive"}, NaiveMode: true}
	root, err := BuildConfigFromRemoteTemplate(settings, base, time.Nanosecond, nil)
	if err != nil {
		t.Fatalf("BuildConfigFromRemoteTemplate: %v", err)
	}
	out, err := root.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	built := string(out)
	if strings.Contains(built, `"vless-reality"`) {
		t.Errorf("the NaiveProxy mode kept another protocol:\n%s", built)
	}
	if !strings.Contains(built, `"username":"`+uuid+`"`) || !strings.Contains(built, `"password":"naivepassword"`) {
		t.Errorf("naive must sign in with the UUID and the shared password:\n%s", built)
	}
	if deps := urltestDeps(t, root); !reflect.DeepEqual(deps, []string{"naive"}) {
		t.Errorf("urltest outbounds = %v, want naive alone", deps)
	}
	// The protocol panel lists the protocols, not naive.
	if got := AvailableProxyTags(base); len(got) != 1 || got[0] != "vless-reality" {
		t.Errorf("AvailableProxyTags = %v, want [vless-reality]", got)
	}
}

// The NaiveProxy mode against a profile without naive says so, instead of
// reading as every protocol switched off.
func TestNaiveModeWithoutNaiveSaysSo(t *testing.T) {
	base := filepath.Join(t.TempDir(), "config.json")
	profile := `{"outbounds":[{"type":"vless","tag":"vless-reality","uuid":""},
	  {"type":"urltest","tag":"auto","outbounds":["vless-reality"]},{"type":"direct","tag":"direct"}]}`
	if err := os.WriteFile(remoteTemplateCachePath(base), []byte(profile), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := BuildConfigFromRemoteTemplate(core.SingboxSection{UUID: "11111111-1111-1111-1111-111111111111", NaiveMode: true}, base, time.Nanosecond, nil)
	if err == nil || !strings.Contains(err.Error(), "no NaiveProxy outbound") {
		t.Fatalf("err = %v, want the missing naive outbound named", err)
	}
}
