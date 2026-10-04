package singbox

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// parseOutbounds is the outbounds array of a profile, as the client reads it.
func parseOutbounds(t *testing.T, body string) []any {
	t.Helper()
	root := NewOrderedMap()
	if err := unmarshalToOrderedMap([]byte(body), root); err != nil {
		t.Fatalf("parse: %v", err)
	}
	raw, ok := root.Get("outbounds")
	if !ok {
		t.Fatal("no outbounds")
	}
	arr, ok := raw.([]any)
	if !ok {
		t.Fatal("outbounds is not an array")
	}
	return arr
}

// The profile that is served today, so the change is anchored to something real.
const liveProfile = `{"outbounds":[
  {"type":"vless","tag":"vless-reality"},
  {"type":"vless","tag":"vless-grpc"},
  {"type":"tuic","tag":"tuic"},
  {"type":"hysteria2","tag":"hysteria2"},
  {"type":"anytls","tag":"anytls"},
  {"type":"urltest","tag":"auto","outbounds":["vless-reality","vless-grpc","tuic","hysteria2","anytls"]},
  {"type":"direct","tag":"direct"}
]}`

func TestTheSelectableTagsAreTheProfilesOwn(t *testing.T) {
	got := ProxyTagsFromOutbounds(parseOutbounds(t, liveProfile), nil)
	want := []string{"vless-reality", "vless-grpc", "tuic", "hysteria2", "anytls"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tags = %v, want %v", got, want)
	}
}

// Plumbing is not a protocol the user chooses between.
func TestUrltestDirectAndBlockAreNotOffered(t *testing.T) {
	got := ProxyTagsFromOutbounds(parseOutbounds(t, `{"outbounds":[
	  {"type":"selector","tag":"select"},
	  {"type":"block","tag":"block"},
	  {"type":"dns","tag":"dns-out"},
	  {"type":"direct","tag":"direct"},
	  {"type":"urltest","tag":"auto"},
	  {"type":"trojan","tag":"trojan-eu"}
	]}`), nil)
	if !reflect.DeepEqual(got, []string{"trojan-eu"}) {
		t.Errorf("tags = %v, want just the trojan outbound", got)
	}
}

// The server changing its mind is the point of all this: a tag renamed, one
// dropped, one added, and a protocol the client has never heard of.
func TestAProfileThatChangedIsFollowed(t *testing.T) {
	got := ProxyTagsFromOutbounds(parseOutbounds(t, `{"outbounds":[
	  {"type":"vless","tag":"vless-reality-2"},
	  {"type":"shadowsocks","tag":"ss-fi"},
	  {"type":"urltest","tag":"auto","outbounds":["ss-fi","vless-reality-2"]},
	  {"type":"direct","tag":"direct"}
	]}`), nil)
	// The urltest names the order the server wants them tried in.
	if !reflect.DeepEqual(got, []string{"ss-fi", "vless-reality-2"}) {
		t.Errorf("tags = %v", got)
	}
}

// An outbound the urltest forgot is still selectable — the list is a preference
// about order, not a second definition of what exists.
func TestATagMissingFromTheUrltestIsStillOffered(t *testing.T) {
	got := ProxyTagsFromOutbounds(parseOutbounds(t, `{"outbounds":[
	  {"type":"vless","tag":"a"},
	  {"type":"tuic","tag":"b"},
	  {"type":"urltest","tag":"auto","outbounds":["b"]}
	]}`), nil)
	if !reflect.DeepEqual(got, []string{"b", "a"}) {
		t.Errorf("tags = %v, want the urltest order first and the rest after", got)
	}
}

func TestWhatGetsEnabled(t *testing.T) {
	available := []string{"vless-reality", "tuic", "anytls"}

	// A configuration with no opinion uses everything, so a fresh install works
	// and a newly added protocol is picked up without touching the menu.
	if got := normalizeEnabledProxyTags(nil, available); !reflect.DeepEqual(got, available) {
		t.Errorf("no choice stored = %v, want all", got)
	}

	// A choice is kept, in the server's order rather than the order it was
	// stored in.
	if got := normalizeEnabledProxyTags([]string{"anytls", "vless-reality"}, available); !reflect.DeepEqual(got, []string{"vless-reality", "anytls"}) {
		t.Errorf("stored choice = %v", got)
	}

	// A tag the server dropped goes quietly; the rest of the choice stands.
	if got := normalizeEnabledProxyTags([]string{"tuic", "hysteria2"}, available); !reflect.DeepEqual(got, []string{"tuic"}) {
		t.Errorf("with a dropped tag = %v", got)
	}

	// Every stored tag gone — the server renamed them all. Falling back to what
	// exists keeps the client working; refusing would mean one server-side
	// rename stops every install until each user reopens a menu.
	if got := normalizeEnabledProxyTags([]string{"gone-1", "gone-2"}, available); !reflect.DeepEqual(got, available) {
		t.Errorf("all tags gone = %v, want all available", got)
	}

	// Nothing offered is nothing to enable; the caller reports it.
	if got := normalizeEnabledProxyTags([]string{"tuic"}, nil); got != nil {
		t.Errorf("no profile = %v, want nil", got)
	}
}

func TestVanishedTagsAreNamed(t *testing.T) {
	got := missingFrom([]string{"tuic", "hysteria2", "anytls"}, []string{"tuic"})
	if !reflect.DeepEqual(got, []string{"hysteria2", "anytls"}) {
		t.Errorf("missingFrom = %v", got)
	}
	if got := missingFrom(nil, []string{"tuic"}); got != nil {
		t.Errorf("nothing asked for = %v", got)
	}
}

// What a user turns off has to stay off across restarts, and an outbound the
// server adds afterwards has to come on. Only remembering the "no" does both;
// a stored list of "yes" cannot tell the two situations apart.
func TestTheChoiceThatIsRememberedIsTheNo(t *testing.T) {
	available := []string{"vless-reality", "tuic", "anytls"}

	// Unticked anytls: it stays off on the next start.
	got := EnabledProxyTags(available, []string{"vless-reality", "tuic"}, []string{"anytls"})
	if !reflect.DeepEqual(got, []string{"vless-reality", "tuic"}) {
		t.Errorf("after unticking anytls = %v", got)
	}

	// The server then adds one. The user never said no to it, so it is on.
	got = EnabledProxyTags([]string{"vless-reality", "tuic", "anytls", "shadowtls-fi"},
		[]string{"vless-reality", "tuic"}, []string{"anytls"})
	if !reflect.DeepEqual(got, []string{"vless-reality", "tuic", "shadowtls-fi"}) {
		t.Errorf("after the server added one = %v", got)
	}

	// Nothing refused: everything the profile offers.
	if got := EnabledProxyTags(available, nil, []string{}); !reflect.DeepEqual(got, available) {
		t.Errorf("nothing refused = %v", got)
	}

	// Everything refused is taken at its word — the builder then says there
	// is nothing to connect through. It used to be overruled with the
	// profile's whole list.
	if got := EnabledProxyTags(available, nil, available); len(got) != 0 {
		t.Errorf("everything refused = %v, want none", got)
	}
}

// A configuration written before the "no" list existed still has to work: the
// positive list is read as it always was.
func TestAnOlderConfigurationIsStillUnderstood(t *testing.T) {
	available := []string{"vless-reality", "tuic", "anytls"}
	got := EnabledProxyTags(available, []string{"tuic"}, nil)
	if !reflect.DeepEqual(got, []string{"tuic"}) {
		t.Errorf("legacy list = %v", got)
	}
}

// A transport the proxies only detour through becomes a choice when the route
// reaches a group that names it, and the route is read from the cached profile.
func TestTheRouteDecidesWhetherATransportIsOffered(t *testing.T) {
	base := filepath.Join(t.TempDir(), "singbox.json")
	profile := `{"outbounds":[
	  {"type":"vless","tag":"a","detour":"transport"},
	  {"type":"shadowtls","tag":"transport","password":"server-owned"},
	  {"type":"selector","tag":"proxy","outbounds":["auto","transport"]},
	  {"type":"urltest","tag":"auto","outbounds":["a"]}
	],"route":{"final":"proxy"}}`
	if err := os.WriteFile(remoteTemplateCachePath(base), []byte(profile), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := AvailableProxyTags(base); !reflect.DeepEqual(got, []string{"a", "transport"}) {
		t.Errorf("tags = %v, want the routed transport offered", got)
	}
}
