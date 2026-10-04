package config

import (
	"encoding/json"
	"os"
	"testing"
)

// naiveProfileJSON mirrors the outbound the staging profile added. Shape copied
// from the served profile: `naive` carries an empty username *and* an empty
// password, which is what makes it the interesting case for a rule that fills
// in every empty field.
const naiveProfileJSON = `{
  "dns": {"servers": [{"tag": "dns-remote", "address": "1.1.1.1"}]},
  "outbounds": [
    {"tag": "vless-reality", "type": "vless", "server": "h", "server_port": 443, "uuid": ""},
    {"tag": "naive", "type": "naive", "server": "nvp.example", "server_port": 443,
     "username": "", "password": "", "quic": false, "udp_over_tcp": {"enabled": true}},
    {"tag": "auto", "type": "urltest", "outbounds": ["vless-reality", "naive"]},
    {"tag": "direct", "type": "direct"}
  ],
  "route": {"rules": [{"protocol": "dns", "action": "hijack-dns"}]}
}`

func outboundByTag(t *testing.T, cfg map[string]interface{}, tag string) map[string]interface{} {
	t.Helper()
	obs, _ := cfg["outbounds"].([]interface{})
	for _, o := range obs {
		m, ok := o.(map[string]interface{})
		if ok && m["tag"] == tag {
			return m
		}
	}
	return nil
}

// The naive outbound is a naiveproxy client, and naiveproxy authenticates with
// a username *and* a password. Only the password is the user's — the username
// is the server's to set, and a client that overwrote it with the UUID would
// authenticate as the wrong account on every connection.
func TestBuildRuntimeConfig_NaiveTakesTheUUIDInItsEmptyFields(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	const uuid = "11111111-1111-1111-1111-111111111111"
	outPath, err := BuildRuntimeConfig(
		[]byte(naiveProfileJSON), RuntimeOptions{
			UserUUID:        uuid,
			OutboundEnabled: map[string]bool{"vless-reality": true, "naive": true},
		},
	)
	if err != nil {
		t.Fatalf("BuildRuntimeConfig: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("runtime config is not valid JSON: %v", err)
	}

	naive := outboundByTag(t, cfg, "naive")
	if naive == nil {
		t.Fatal("the naive outbound was dropped from the built config")
	}
	if naive["password"] != uuid {
		t.Errorf("naive password = %v, want the user UUID", naive["password"])
	}
	// Every empty field of an outbound is a placeholder for the credential now,
	// because naming the fields meant a client release before a new protocol
	// could authenticate. naive is the one outbound that ships two of them, so
	// its username is filled in as well.
	if naive["username"] != uuid {
		t.Errorf("naive username = %v, want the user UUID", naive["username"])
	}
	// Fields the client has no business rewriting.
	if naive["server"] != "nvp.example" || naive["quic"] != false {
		t.Errorf("naive outbound was altered beyond its credential: %v", naive)
	}

	// An outbound the client cannot fail over to is an outbound the user cannot
	// reach: the urltest has to keep listing it.
	auto := outboundByTag(t, cfg, "auto")
	if auto == nil {
		t.Fatal("no urltest in the built config")
	}
	list, _ := auto["outbounds"].([]interface{})
	var found bool
	for _, v := range list {
		if v == "naive" {
			found = true
		}
	}
	if !found {
		t.Errorf("urltest outbounds = %v, want naive among them", list)
	}
}

// Turning naive off has to remove it from the config outright, not leave a
// credential-less outbound that sing-box would refuse to load.
func TestBuildRuntimeConfig_NaiveDisabledIsRemoved(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	outPath, err := BuildRuntimeConfig(
		[]byte(naiveProfileJSON), RuntimeOptions{
			UserUUID:        "11111111-1111-1111-1111-111111111111",
			OutboundEnabled: map[string]bool{"vless-reality": true, "naive": false},
		},
	)
	if err != nil {
		t.Fatalf("BuildRuntimeConfig: %v", err)
	}
	data, _ := os.ReadFile(outPath)
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if outboundByTag(t, cfg, "naive") != nil {
		t.Error("a disabled naive outbound is still in the built config")
	}
	auto := outboundByTag(t, cfg, "auto")
	list, _ := auto["outbounds"].([]interface{})
	for _, v := range list {
		if v == "naive" {
			t.Errorf("urltest still fails over to the disabled naive outbound: %v", list)
		}
	}
}

// The profile is the only place the client learns which outbounds exist, so a
// tag it has never heard of has to reach the user's checkbox list.
func TestProxyTagsFromJSON_IncludesNaive(t *testing.T) {
	tags := ProxyTagsFromJSON([]byte(naiveProfileJSON))
	want := []string{"vless-reality", "naive"}
	if len(tags) != len(want) {
		t.Fatalf("tags = %v, want %v", tags, want)
	}
	for i := range want {
		if tags[i] != want[i] {
			t.Fatalf("tags = %v, want %v (urltest order first)", tags, want)
		}
	}
}
