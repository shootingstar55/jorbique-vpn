package config

import (
	"encoding/json"
	"os"
	"testing"

	"jorbiquevpn/internal/assets"
	"jorbiquevpn/internal/jsonorder"
)

func TestBuildRuntimeConfig_smoke(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	// A profile of its own: the template carries only what this machine
	// decides — the tunnel, the log, the api, the cache — and the protocols,
	// the routes and the resolvers all arrive from the server.
	profile := []byte(`{
	  "dns": {"servers":[{"tag":"dns-local","type":"local"}],"rules":[],"final":"dns-local"},
	  "outbounds": [
	    {"type":"vless","tag":"vless-reality","uuid":""},
	    {"type":"tuic","tag":"tuic","uuid":""},
	    {"type":"urltest","tag":"auto","outbounds":["vless-reality","tuic"]},
	    {"type":"direct","tag":"direct"}
	  ],
	  "route": {"rules":[],"final":"auto"}
	}`)
	enabled := map[string]bool{
		"vless-reality": true,
		"tuic":          true,
	}
	path, err := BuildRuntimeConfig(profile, RuntimeOptions{
		UserUUID:        "00000000-0000-0000-0000-000000000001",
		OutboundEnabled: enabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(path) }()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 100 {
		t.Fatalf("runtime_config.json too small: %d bytes", len(data))
	}
	var cfg *jsonorder.Map
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := mustField[any](cfg, "outbounds"); !ok {
		t.Fatal("expected outbounds in runtime config")
	}
}

// The two halves: the profile brings what the server decides, the template only
// what this machine does. The template used to carry a copy of the first half
// too — protocols, routes and resolvers that the profile always overrode — and
// the names in it drifted out of date without anyone noticing, because nothing
// ever read them.
func TestMergeRemoteWithTunShell_smoke(t *testing.T) {
	remote := []byte(`{"outbounds":[{"type":"direct","tag":"direct"}],"route":{"final":"direct"}}`)
	cfg, err := MergeRemoteWithTunShell(remote)
	if err != nil {
		t.Fatal(err)
	}
	for _, fromProfile := range []string{"outbounds", "route"} {
		if !cfg.Has(fromProfile) {
			t.Errorf("%s did not survive the merge", fromProfile)
		}
	}
	for _, fromTemplate := range []string{"log", "inbounds", "services", "experimental"} {
		if !cfg.Has(fromTemplate) {
			t.Errorf("the template's %s was not filled in", fromTemplate)
		}
	}
	// And the template no longer smuggles in a second copy of the server's half.
	tpl, err := assets.FS.ReadFile("tun.json")
	if err != nil {
		t.Fatal(err)
	}
	var shell *jsonorder.Map
	if err := json.Unmarshal(tpl, &shell); err != nil {
		t.Fatal(err)
	}
	for _, serverSide := range []string{"dns", "outbounds", "route", "endpoints"} {
		if shell.Has(serverSide) {
			t.Errorf("the template still carries %q, which the profile decides", serverSide)
		}
	}
}
