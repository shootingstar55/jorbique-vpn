package routerules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"jorbiquevpn/internal/macos/singbox"
)

// writeProfile lays down a config shaped like the downloaded profile.
func writeProfile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "singbox.json")
	root := singbox.NewOrderedMap()
	if err := root.UnmarshalJSON([]byte(`{
  "log": {"level":"warn","timestamp":true},
  "dns": {
    "servers": [
      {"tag":"doh-cloudflare","type":"https","server":"1.1.1.1","server_port":443,"path":"/dns-query","tls":{"enabled":true,"server_name":"cloudflare-dns.com"}},
      {"tag":"dns-local","type":"local"}
    ],
    "rules": [
      {"domain_regex":[".*\\.ru$"],"action":"route","server":"dns-local"}
    ],
    "final": "doh-cloudflare",
    "strategy": "ipv4_only"
  },
  "inbounds": [{"type":"tun","tag":"tun-in","address":["100.64.0.2/24"],"auto_route":true,"stack":"gvisor","mtu":1280}],
  "outbounds": [{"type":"direct","tag":"direct"}],
  "route": {"rules":[{"action":"sniff","sniffer":["http","tls"],"timeout":"500ms"}],"final":"auto"}
}`)); err != nil {
		t.Fatal(err)
	}
	if err := singbox.WriteJSONFileAtomically(root, p); err != nil {
		t.Fatal(err)
	}
	return p
}

func patchedDNSRules(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		DNS struct {
			Rules []map[string]any `json:"rules"`
		} `json:"dns"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("patched config is not valid JSON: %v", err)
	}
	return raw.DNS.Rules
}

// A name that goes out directly resolves on the local resolver, ahead of the
// profile's own rules, matched exactly and as a dotted suffix: a bare
// "example.com" in domain_suffix would also claim "xexample.com".
func TestPatchSendsDirectNamesToTheLocalResolverFirst(t *testing.T) {
	p := writeProfile(t)
	if err := PatchSingboxRouteRules(p, []string{"example.com", "corp.example"}, nil, nil); err != nil {
		t.Fatal(err)
	}

	rules := patchedDNSRules(t, p)
	if len(rules) != 2 {
		t.Fatalf("got %d DNS rules, want ours and the profile's: %v", len(rules), rules)
	}
	want := map[string]any{
		"domain":        []any{"example.com", "corp.example"},
		"domain_suffix": []any{".example.com", ".corp.example"},
		"action":        "route",
		"server":        "dns-local",
	}
	if !reflect.DeepEqual(rules[0], want) {
		t.Errorf("direct rule = %v, want %v", rules[0], want)
	}
	if _, ok := rules[1]["domain_regex"]; !ok {
		t.Errorf("the profile's rule was not kept second: %v", rules[1])
	}
}

// Saving again replaces the rule this package wrote rather than stacking a
// second one on top, and an empty list takes it away.
func TestPatchReplacesItsOwnDNSRule(t *testing.T) {
	p := writeProfile(t)
	for _, direct := range [][]string{{"old.example"}, {"new.example"}} {
		if err := PatchSingboxRouteRules(p, direct, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	rules := patchedDNSRules(t, p)
	if len(rules) != 2 || !reflect.DeepEqual(rules[0]["domain"], []any{"new.example"}) {
		t.Fatalf("rules = %v, want the new name once and the profile's rule", rules)
	}

	if err := PatchSingboxRouteRules(p, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if rules := patchedDNSRules(t, p); len(rules) != 1 {
		t.Errorf("rules = %v, want only the profile's left", rules)
	}
}

// The rules array is spliced into an already-formatted file, so it has to line
// up with the section around it.
func TestPatchSingboxRulesKeepIndentation(t *testing.T) {
	p := writeProfile(t)
	if err := PatchSingboxRouteRules(p, []string{"example.com"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if strings.HasSuffix(strings.TrimSpace(line), `"rules": [`) && indent != 4 {
			t.Errorf(`"rules" key indented %d spaces, want 4: %q`, indent, line)
		}
		if strings.TrimSpace(line) == `"action": "route",` && indent != 8 {
			t.Errorf("rule body indented %d spaces, want 8: %q", indent, line)
		}
	}
}

// The profile's own key order is the author's; the writer must not reshuffle
// what it has no opinion about (it used to alphabetize, turning a DNS server
// into path/server/server_port/tag/tls).
func TestWriterKeepsProfileKeyOrder(t *testing.T) {
	p := writeProfile(t)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, pair := range [][2]string{
		{`"tag": "doh-cloudflare"`, `"type": "https"`},
		{`"type": "https"`, `"server": "1.1.1.1"`},
		{`"server_port": 443`, `"path": "/dns-query"`},
	} {
		first, second := strings.Index(text, pair[0]), strings.Index(text, pair[1])
		if first < 0 || second < 0 || first > second {
			t.Errorf("expected %s before %s", pair[0], pair[1])
		}
	}
}

// A DNS rule the profile narrows further — a query type, a source filter — is
// not one this package wrote, and dropping it because it merely names the
// local resolver deleted the profile's intent.
func TestPatchKeepsProfileDNSRules(t *testing.T) {
	p := filepath.Join(t.TempDir(), "singbox.json")
	body := `{
  "dns": {
    "servers": [],
    "rules": [
      {"query_type":["HTTPS"],"domain_suffix":[".example.com"],"action":"route","server":"dns-local"},
      {"domain_suffix":[".legacy.example"],"action":"route","server":"dns-local","source_ip_cidr":["10.40.0.0/16"]}
    ],
    "final": "doh-cloudflare"
  },
  "outbounds": [{"type":"direct","tag":"direct"}],
  "route": {"rules": [], "final": "auto"}
}`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := PatchSingboxRouteRules(p, []string{"example.com"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if rules := patchedDNSRules(t, p); len(rules) != 2 {
		t.Fatalf("the profile's narrowed DNS rules were dropped: %v", rules)
	}
}

// Rules outlive the servers they name: they are carried across a rebuild from
// the file on disk, while the servers come from the freshly downloaded profile.
// A rule naming a resolver the config no longer has can never match, so it
// goes; a rule that names no server at all is nobody's business here.
func TestPatchDropsDNSRulesNamingAServerThatIsGone(t *testing.T) {
	p := filepath.Join(t.TempDir(), "singbox.json")
	body := `{
  "dns": {
    "servers": [
      {"tag":"doh","type":"https","server":"1.1.1.1"},
      {"tag":"dns-local","type":"local"}
    ],
    "rules": [
      {"domain_regex":[".*\\.ru$"],"action":"route","server":"dns-local"},
      {"domain":["lan"],"domain_suffix":[".lan"],"action":"route","server":"gone-resolver"},
      {"action":"reject","domain":["blocked.example"]}
    ],
    "final": "doh"
  },
  "outbounds": [{"type":"direct","tag":"direct"}],
  "route": {"rules": [], "final": "direct"}
}`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := PatchSingboxRouteRules(p, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	rules := patchedDNSRules(t, p)
	for _, r := range rules {
		if r["server"] == "gone-resolver" {
			t.Errorf("a rule naming a server that is gone survived: %v", r)
		}
	}
	if len(rules) != 2 {
		t.Errorf("rules = %v, want the live rule and the reject kept", rules)
	}
}
