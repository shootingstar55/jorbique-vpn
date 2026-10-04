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

// naive is an ordinary selectable outbound here: the macOS sing-box build has
// naiveproxy in it natively, so nothing about this tag is special to the client.
func TestNaiveIsAnOrdinarySelectableTag(t *testing.T) {
	got := ProxyTagsFromOutbounds(parseOutbounds(t, naiveProfile), nil)
	want := []string{"vless-reality", "anytls", "naive"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("selectable tags = %v, want %v", got, want)
	}
}

// naiveproxy authenticates with a username *and* a password. Only the password
// is the user's — the username is the server's to set, and a client that put
// the UUID there would authenticate as the wrong account every time.
func TestNaiveTakesTheUUIDInPasswordOnly(t *testing.T) {
	base := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(remoteTemplateCachePath(base), []byte(naiveProfile), 0o644); err != nil {
		t.Fatal(err)
	}

	const uuid = "11111111-1111-1111-1111-111111111111"
	// A 1ns timeout stands in for an unreachable server, so the cached profile
	// above is what gets built.
	root, err := BuildConfigFromRemoteTemplate(core.SingboxSection{UUID: uuid}, base, time.Nanosecond, nil)
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
	if !strings.Contains(built, `"password":"`+uuid+`"`) {
		t.Errorf("naive did not get the user UUID as its password:\n%s", built)
	}
	// Every empty field of an outbound is a placeholder for the credential now,
	// because naming the fields meant a client release before a new protocol
	// could authenticate. naive is the one outbound that ships two of them, so
	// its username is filled in as well.
	if strings.Contains(built, `"username":""`) {
		t.Errorf("naive's username was left empty:\n%s", built)
	}
	// And it stays in the failover list, or the user cannot actually reach it.
	// Checked on the parsed urltest rather than on the text, where the tag on
	// the outbound itself would match and prove nothing.
	if deps := urltestDeps(t, root); !contains(deps, "naive") {
		t.Errorf("urltest outbounds = %v, want naive among them", deps)
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

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// The NaiveProxy mode builds from the naive profile, kept in a cache of its
// own, and the protocol toggles — answers about the main profile — have no say
// in it.
func TestNaiveModeBuildsFromTheNaiveProfile(t *testing.T) {
	base := filepath.Join(t.TempDir(), "config.json")
	main := `{"outbounds":[{"type":"vless","tag":"vless-reality","uuid":""},
	  {"type":"urltest","tag":"auto","outbounds":["vless-reality"]},{"type":"direct","tag":"direct"}]}`
	naive := `{"outbounds":[{"type":"naive","tag":"naive","server":"192.0.2.1","server_port":443,
	  "username":"","password":"naivepassword","insecure_concurrency":2,"udp_over_tcp":true},
	  {"type":"urltest","tag":"auto","outbounds":["naive"]},{"type":"direct","tag":"direct"}]}`
	if err := os.WriteFile(remoteTemplateCachePath(base), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(naiveTemplateCachePath(base), []byte(naive), 0o644); err != nil {
		t.Fatal(err)
	}

	const uuid = "11111111-1111-1111-1111-111111111111"
	settings := core.SingboxSection{UUID: uuid, DisabledOutbounds: []string{"naive", "vless-reality"}, NaiveMode: true}
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
		t.Errorf("the NaiveProxy mode built the main profile:\n%s", built)
	}
	if !strings.Contains(built, `"username":"`+uuid+`"`) || !strings.Contains(built, `"password":"naivepassword"`) {
		t.Errorf("naive must sign in with the UUID and the shared password:\n%s", built)
	}
	if deps := urltestDeps(t, root); !contains(deps, "naive") {
		t.Errorf("urltest outbounds = %v, want naive", deps)
	}
	// The protocol panel still lists the main profile.
	if got := AvailableProxyTags(base); len(got) != 1 || got[0] != "vless-reality" {
		t.Errorf("AvailableProxyTags = %v, want the main profile's [vless-reality]", got)
	}
}
