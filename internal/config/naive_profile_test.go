package config

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
)

// naiveProfileJSON carries naive inside "auto", the way a profile from before
// the NaiveProxy mode did: `naive` has an empty username *and* an empty
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

// servedProfileJSON is the profile as the server publishes it now: naive beside
// the protocols but outside "auto", with the shared password filled in.
const servedProfileJSON = `{
  "outbounds": [
    {"tag": "vless-reality", "type": "vless", "server": "h", "server_port": 443, "uuid": ""},
    {"tag": "naive", "type": "naive", "server": "h", "server_port": 443,
     "username": "", "password": "shared-naive-password", "insecure_concurrency": 2, "udp_over_tcp": true},
    {"tag": "auto", "type": "urltest", "outbounds": ["vless-reality"]},
    {"tag": "direct", "type": "direct"}
  ],
  "route": {"final": "auto"}
}`

func buildToMap(t *testing.T, profile string, options RuntimeOptions) map[string]interface{} {
	t.Helper()
	t.Setenv("APPDATA", t.TempDir())
	outPath, err := BuildRuntimeConfig([]byte(profile), options)
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
	return cfg
}

func autoMembers(t *testing.T, cfg map[string]interface{}) []interface{} {
	t.Helper()
	auto := outboundByTag(t, cfg, "auto")
	if auto == nil {
		t.Fatal("no urltest in the built config")
	}
	list, _ := auto["outbounds"].([]interface{})
	return list
}

// The NaiveProxy mode runs on naive alone: every other protocol goes, whatever
// its toggle says, and "auto" is left failing over to naive only.
func TestBuildRuntimeConfig_NaiveModeKeepsNaiveAlone(t *testing.T) {
	const uuid = "11111111-1111-1111-1111-111111111111"
	cfg := buildToMap(t, servedProfileJSON, RuntimeOptions{
		UserUUID:        uuid,
		OutboundEnabled: map[string]bool{"vless-reality": true},
		NaiveMode:       true,
	})
	if outboundByTag(t, cfg, "vless-reality") != nil {
		t.Error("the NaiveProxy mode kept another protocol")
	}
	naive := outboundByTag(t, cfg, "naive")
	if naive == nil {
		t.Fatal("the naive outbound was dropped from the built config")
	}
	// naiveproxy signs in with a username and a password: the username is the
	// user's UUID, the password the one every user shares.
	if naive["username"] != uuid || naive["password"] != "shared-naive-password" {
		t.Errorf("naive signs in as %v / %v, want the UUID and the shared password", naive["username"], naive["password"])
	}
	if list := autoMembers(t, cfg); len(list) != 1 || list[0] != "naive" {
		t.Errorf("urltest outbounds = %v, want naive alone", list)
	}
}

// A profile that still lists naive in "auto" gets the same build: the mode
// decides, not where the server put it.
func TestBuildRuntimeConfig_NaiveModeFromAnOlderProfileShape(t *testing.T) {
	const uuid = "11111111-1111-1111-1111-111111111111"
	cfg := buildToMap(t, naiveProfileJSON, RuntimeOptions{UserUUID: uuid, NaiveMode: true})
	naive := outboundByTag(t, cfg, "naive")
	if naive == nil {
		t.Fatal("the naive outbound was dropped from the built config")
	}
	// Every empty field of an outbound is a placeholder for the credential.
	if naive["username"] != uuid || naive["password"] != uuid {
		t.Errorf("naive credential = %v / %v, want the user UUID in both empty fields", naive["username"], naive["password"])
	}
	// Fields the client has no business rewriting.
	if naive["server"] != "nvp.example" || naive["quic"] != false {
		t.Errorf("naive outbound was altered beyond its credential: %v", naive)
	}
	if list := autoMembers(t, cfg); len(list) != 1 || list[0] != "naive" {
		t.Errorf("urltest outbounds = %v, want naive alone", list)
	}
}

// The protocols mode never starts naive, even with its toggle on and even when
// the profile lists it in "auto": in the failover group it would run beside
// every other protocol, and it is the heaviest of them.
func TestBuildRuntimeConfig_ProtocolsModeLeavesNaiveOut(t *testing.T) {
	for name, profile := range map[string]string{"served": servedProfileJSON, "naive in auto": naiveProfileJSON} {
		t.Run(name, func(t *testing.T) {
			cfg := buildToMap(t, profile, RuntimeOptions{
				UserUUID:        "11111111-1111-1111-1111-111111111111",
				OutboundEnabled: map[string]bool{"vless-reality": true, "naive": true},
			})
			if outboundByTag(t, cfg, "naive") != nil {
				t.Error("the protocols mode kept the naive outbound")
			}
			if outboundByTag(t, cfg, "vless-reality") == nil {
				t.Error("the protocols mode dropped vless-reality")
			}
			for _, v := range autoMembers(t, cfg) {
				if v == "naive" {
					t.Errorf("urltest still fails over to naive: %v", autoMembers(t, cfg))
				}
			}
		})
	}
}

// The NaiveProxy mode against a profile without naive says so, instead of
// reading as every protocol switched off.
func TestBuildRuntimeConfig_NaiveModeWithoutNaiveSaysSo(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	profile := `{"outbounds":[{"tag":"vless-reality","type":"vless","uuid":""},
	  {"tag":"auto","type":"urltest","outbounds":["vless-reality"]},{"tag":"direct","type":"direct"}],
	  "route":{"final":"auto"}}`
	_, err := BuildRuntimeConfig([]byte(profile), RuntimeOptions{
		UserUUID: "11111111-1111-1111-1111-111111111111", NaiveMode: true,
	})
	if !errors.Is(err, ErrNoNaiveOutbound) {
		t.Fatalf("err = %v, want ErrNoNaiveOutbound", err)
	}
}

// NaiveProxy is a mode, not a protocol to toggle, so the list the settings
// window draws checkboxes from leaves it out.
func TestProtocolTagsFromJSON_LeavesNaiveOut(t *testing.T) {
	for _, profile := range []string{servedProfileJSON, naiveProfileJSON} {
		if tags := ProtocolTagsFromJSON([]byte(profile)); !reflect.DeepEqual(tags, []string{"vless-reality"}) {
			t.Errorf("tags = %v, want [vless-reality]", tags)
		}
	}
}
