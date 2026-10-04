package config

import (
	"reflect"
	"testing"

	"jorbiquevpn/internal/jsonorder"
)

func tagsOf(t *testing.T, body string) []string {
	t.Helper()
	cfg := jsonorder.New()
	if err := jsonorder.Unmarshal([]byte(body), cfg); err != nil {
		t.Fatalf("parse: %v", err)
	}
	return ProxyTagsFromConfig(cfg)
}

// The profile the server serves today, so this is anchored to something real.
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
	got := tagsOf(t, liveProfile)
	want := []string{"vless-reality", "vless-grpc", "tuic", "hysteria2", "anytls"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tags = %v, want %v", got, want)
	}
}

// Plumbing is not something a user picks between, and a protocol this client
// has never heard of is.
func TestPlumbingIsSkippedAndUnknownProtocolsAreNot(t *testing.T) {
	got := tagsOf(t, `{"outbounds":[
	  {"type":"selector","tag":"select"},
	  {"type":"block","tag":"block"},
	  {"type":"dns","tag":"dns-out"},
	  {"type":"direct","tag":"direct"},
	  {"type":"urltest","tag":"auto"},
	  {"type":"shadowtls","tag":"shadowtls-fi"}
	]}`)
	if !reflect.DeepEqual(got, []string{"shadowtls-fi"}) {
		t.Errorf("tags = %v", got)
	}
}

// The urltest states the order the server wants them tried in; an outbound it
// forgot is still selectable.
func TestOrderComesFromTheUrltestThenTheArray(t *testing.T) {
	got := tagsOf(t, `{"outbounds":[
	  {"type":"vless","tag":"a"},
	  {"type":"tuic","tag":"b"},
	  {"type":"urltest","tag":"auto","outbounds":["b"]}
	]}`)
	if !reflect.DeepEqual(got, []string{"b", "a"}) {
		t.Errorf("tags = %v", got)
	}
}

// The credential goes in whichever field the profile carries, so a protocol
// added on the server does not need a client release to be usable.
func TestTheCredentialFollowsTheProfilesFields(t *testing.T) {
	cfg := jsonorder.New()
	if err := jsonorder.Unmarshal([]byte(`{"outbounds":[
	  {"type":"vless","tag":"a","uuid":""},
	  {"type":"hysteria2","tag":"b","password":""},
	  {"type":"shadowtls","tag":"c","password":""},
	  {"type":"direct","tag":"direct"}
	]}`), cfg); err != nil {
		t.Fatal(err)
	}
	if err := ApplyUserUUID(cfg, "the-uuid", ProxyTagsFromConfig(cfg)); err != nil {
		t.Fatalf("ApplyUserUUID: %v", err)
	}
	obs := field[[]any](cfg, "outbounds")
	if got := field[any](obs[0].(*jsonorder.Map), "uuid"); got != "the-uuid" {
		t.Errorf("uuid outbound = %v", got)
	}
	if got := field[any](obs[1].(*jsonorder.Map), "password"); got != "the-uuid" {
		t.Errorf("password outbound = %v", got)
	}
	if got := field[any](obs[2].(*jsonorder.Map), "password"); got != "the-uuid" {
		t.Errorf("unknown protocol did not get the credential: %v", got)
	}
	if _, ok := obs[3].(*jsonorder.Map).Get("uuid"); ok {
		t.Error("direct was given a credential")
	}
}

// An outbound with nowhere to put the credential is a profile we cannot build
// from, and saying so beats shipping a config that cannot authenticate.
func TestAnOutboundWithNoCredentialFieldIsReported(t *testing.T) {
	cfg := jsonorder.New()
	if err := jsonorder.Unmarshal([]byte(`{"outbounds":[{"type":"newthing","tag":"x"}]}`), cfg); err != nil {
		t.Fatal(err)
	}
	err := ApplyUserUUID(cfg, "the-uuid", ProxyTagsFromConfig(cfg))
	if err == nil {
		t.Fatal("no error for an outbound with no uuid or password field")
	}
}

// A transport the proxies only detour through becomes a choice when the route
// reaches a group that names it, and the route is read from the profile itself.
func TestTheRouteDecidesWhetherATransportIsOffered(t *testing.T) {
	const outbounds = `"outbounds":[
	  {"type":"vless","tag":"a","detour":"transport"},
	  {"type":"shadowtls","tag":"transport","password":"server-owned"},
	  {"type":"selector","tag":"proxy","outbounds":["auto","transport"]},
	  {"type":"urltest","tag":"auto","outbounds":["a"]}
	]`
	if got := tagsOf(t, `{`+outbounds+`}`); !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("unrouted: tags = %v, want the transport left alone", got)
	}
	if got := tagsOf(t, `{`+outbounds+`,"route":{"final":"proxy"}}`); !reflect.DeepEqual(got, []string{"a", "transport"}) {
		t.Errorf("routed: tags = %v, want the transport offered", got)
	}
}
