package routerules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jorbiquevpn/internal/macos/core"
)

// legacyCfgWithSingboxVPN lists a VPN of type "singbox" inline — so the
// characters "singbox" appear as a *value*, ahead of the section of the same
// name.
const legacyCfgWithSingboxVPN = `{
  "vpns" : [
    {
      "name" : "Jorbique VPN",
      "type" : "singbox",
      "file" : "cfg/singbox.json"
    }
  ],
  "singbox" : {
    "uuid": "5fdcc635-500b-4c5e-b6df-f88262f82e92",
    "enabled_outbounds": ["vless-reality", "tuic"]
  }
}`

func writeCfg(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "vpn_cfg.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func mustStillParse(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(core.StripTrailingCommas(data), &raw); err != nil {
		t.Fatalf("the config is no longer readable: %v\n%s", err, data)
	}
	return raw
}

// Adding a direct domain to a config that has none used to insert the key at
// the first occurrence of the characters "singbox" — which, in a config that
// lists a sing-box VPN, is the *value* of its "type". The key landed between
// "type" and "singbox", splitting the pair, and from then on nothing could read
// the file: the settings panel came up empty, saving appended a duplicate uuid
// and reported success, and it did that once per attempt.
func TestAddingADirectDomainKeepsTheConfigReadable(t *testing.T) {
	path := writeCfg(t, legacyCfgWithSingboxVPN)

	if err := PatchVpnCfgRouteRules(path, []string{"example.com"}, nil, nil); err != nil {
		t.Fatalf("patch: %v", err)
	}
	raw := mustStillParse(t, path)

	// The VPN entry is untouched: its type still says what it is.
	vpns, _ := raw["vpns"].([]any)
	if len(vpns) != 1 {
		t.Fatalf("vpns = %v", vpns)
	}
	last, _ := vpns[0].(map[string]any)
	if last["type"] != "singbox" {
		t.Errorf(`the sing-box VPN's type is now %v — the insert landed inside it`, last["type"])
	}
	// And the domain arrived.
	got, _ := raw["direct_domains"].([]any)
	if len(got) != 1 || got[0] != "example.com" {
		t.Errorf("direct_domains = %v", got)
	}
	// The section it was meant to go in front of is still an object.
	if _, ok := raw["singbox"].(map[string]any); !ok {
		t.Errorf("the singbox section is no longer an object: %v", raw["singbox"])
	}
}

// The same for the other two lists, which take the same path.
func TestAddingBypassListsKeepsTheConfigReadable(t *testing.T) {
	path := writeCfg(t, legacyCfgWithSingboxVPN)
	if err := PatchVpnCfgRouteRules(path, nil, []string{"zoom"}, []string{"/usr/bin/ssh"}); err != nil {
		t.Fatalf("patch: %v", err)
	}
	raw := mustStillParse(t, path)
	if p, _ := raw["direct_bypass_processes"].([]any); len(p) != 1 || p[0] != "zoom" {
		t.Errorf("direct_bypass_processes = %v", raw["direct_bypass_processes"])
	}
	if p, _ := raw["direct_bypass_paths"].([]any); len(p) != 1 {
		t.Errorf("direct_bypass_paths = %v", raw["direct_bypass_paths"])
	}
}

// A config that is already broken must not be edited further. Every save used
// to append another uuid and report success, so a machine accumulated twenty of
// them while the panel stayed empty and nothing said why.
func TestAConfigThatDoesNotParseIsNotEdited(t *testing.T) {
	broken := strings.Replace(legacyCfgWithSingboxVPN,
		`"type" : "singbox"`,
		`"type" :   "direct_domains": [],`+"\n\n"+`"singbox"`, 1)
	path := writeCfg(t, broken)
	before, _ := os.ReadFile(path)

	uuid := "11111111-1111-1111-1111-111111111111"
	err := PatchVpnCfgSingboxUUIDAndOutbounds(path, &uuid, []string{"tuic"}, nil, nil)
	if err == nil {
		t.Fatal("saving into an unreadable config reported success")
	}
	if !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("the error does not say what is wrong: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("the unreadable config was written to anyway")
	}
}

// Saving into a healthy config still works, and replaces rather than appends —
// the duplicate-uuid pile is what an unreadable file produced, and a readable
// one must never grow one.
func TestSavingReplacesTheUUIDInsteadOfAppending(t *testing.T) {
	path := writeCfg(t, legacyCfgWithSingboxVPN)
	uuid := "22222222-2222-2222-2222-222222222222"
	for i := 0; i < 3; i++ {
		if err := PatchVpnCfgSingboxUUIDAndOutbounds(path, &uuid, []string{"tuic"}, nil, nil); err != nil {
			t.Fatalf("save %d: %v", i+1, err)
		}
	}
	data, _ := os.ReadFile(path)
	if n := strings.Count(string(data), `"uuid"`); n != 1 {
		t.Errorf(`the config has %d "uuid" keys after three saves, want 1:\n%s`, n, data)
	}
	raw := mustStillParse(t, path)
	sb, _ := raw["singbox"].(map[string]any)
	if sb["uuid"] != uuid {
		t.Errorf("uuid = %v, want the one just saved", sb["uuid"])
	}
}
