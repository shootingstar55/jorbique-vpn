package app

import (
	"encoding/json"
	"os"
	"testing"

	"jorbiquevpn/internal/config"
	"jorbiquevpn/internal/server"
)

// The profile as the server publishes it: the protocols in "auto", and naive
// beside them with an empty username for the user's UUID and the password
// every user shares.
const naiveModeProfile = `{
  "outbounds": [
    {"type": "vless", "tag": "vless-reality", "server": "192.0.2.1", "server_port": 443, "uuid": ""},
    {"type": "naive", "tag": "naive", "server": "192.0.2.1", "server_port": 443,
     "username": "", "password": "naivepassword", "insecure_concurrency": 2, "udp_over_tcp": true,
     "tls": {"enabled": true, "server_name": "nvp.example"}},
    {"type": "urltest", "tag": "auto", "outbounds": ["vless-reality"]},
    {"type": "direct", "tag": "direct"}
  ],
  "route": {"final": "auto"}
}`

func builtOutbounds(t *testing.T, path string) map[string]map[string]any {
	t.Helper()
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
	byTag := map[string]map[string]any{}
	for _, o := range cfg.Outbounds {
		tag, _ := o["tag"].(string)
		byTag[tag] = o
	}
	return byTag
}

func TestApp_NaiveModeIsSavedAndReported(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.SetVpnUserConfig(appTestUUID, allOutboundsEnabled()); err != nil {
		t.Fatal(err)
	}
	if a.GetVpnUserConfig().NaiveMode {
		t.Fatal("a fresh client must start in the protocols mode")
	}
	if _, err := a.SetNaiveMode(true); err != nil {
		t.Fatalf("SetNaiveMode: %v", err)
	}
	if !a.GetVpnUserConfig().NaiveMode {
		t.Error("the form does not report the NaiveProxy mode")
	}
	reloaded, err := server.Load(a.dataDir)
	if err != nil || !reloaded.GetNaiveMode() {
		t.Errorf("the mode was not persisted: %v / %v", err, reloaded.GetNaiveMode())
	}
}

// The toggles answer for the protocols only, so every one of them off does
// not stop the NaiveProxy mode from connecting.
func TestApp_NaiveModeIgnoresTheProtocolToggles(t *testing.T) {
	a := newTestApp(t)
	if err := os.WriteFile(config.RemoteSingboxCachePath(), []byte(`{
	  "outbounds":[{"type":"vless","tag":"vless-reality"},{"type":"tuic","tag":"tuic"}]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetVpnUserConfig(appTestUUID, map[string]bool{"vless-reality": false, "tuic": false}); err != nil {
		t.Fatal(err)
	}
	if a.HasVpnUserConfig() {
		t.Fatal("every protocol off has nothing to route through")
	}
	if _, err := a.SetNaiveMode(true); err != nil {
		t.Fatal(err)
	}
	if !a.HasVpnUserConfig() {
		t.Error("the NaiveProxy mode routes through naive whatever the toggles say")
	}
}

func TestApp_NaiveModeBuildsFromNaiveAloneWithTheUUIDAsUsername(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	a := newTestApp(t)
	// A protocol toggle on must not bring its protocol into this build.
	if _, err := a.SetVpnUserConfig(appTestUUID, map[string]bool{"vless-reality": true}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetNaiveMode(true); err != nil {
		t.Fatal(err)
	}
	path, err := a.buildRuntimeConfig([]byte(naiveModeProfile))
	if err != nil {
		t.Fatalf("buildRuntimeConfig: %v", err)
	}
	obs := builtOutbounds(t, path)
	if _, kept := obs["vless-reality"]; kept {
		t.Error("the NaiveProxy mode kept another protocol")
	}
	naive := obs["naive"]
	if naive == nil {
		t.Fatal("the naive outbound was dropped")
	}
	if naive["username"] != appTestUUID || naive["password"] != "naivepassword" {
		t.Errorf("naive signs in as %v / %v, want the UUID and the shared password", naive["username"], naive["password"])
	}
}

// Out of the NaiveProxy mode the same profile builds without naive, its toggle
// or not.
func TestApp_ProtocolsModeLeavesNaiveOut(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	a := newTestApp(t)
	if _, err := a.SetVpnUserConfig(appTestUUID, map[string]bool{"vless-reality": true, "naive": true}); err != nil {
		t.Fatal(err)
	}
	path, err := a.buildRuntimeConfig([]byte(naiveModeProfile))
	if err != nil {
		t.Fatalf("buildRuntimeConfig: %v", err)
	}
	obs := builtOutbounds(t, path)
	if _, kept := obs["naive"]; kept {
		t.Error("the protocols mode kept naive")
	}
	if obs["vless-reality"]["uuid"] != appTestUUID {
		t.Errorf("vless-reality = %v, want it kept with the UUID", obs["vless-reality"])
	}
}
