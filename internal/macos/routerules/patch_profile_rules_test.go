package routerules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeProfileWithDirectRules lays down a profile carrying the three direct
// rules that matter here: a process bypass the organisation ships, the
// corporate zones going direct (domain + domain_suffix), and a plain
// domain-only rule of the kind this package writes itself.
func writeProfileWithDirectRules(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "singbox.json")
	body := `{
  "dns": {"servers": [], "rules": []},
  "outbounds": [{"type":"direct","tag":"direct"}],
  "route": {
    "rules": [
      {"action":"sniff","sniffer":["http"],"timeout":"500ms"},
      {"process_name":["Reddy.exe","Reddy"],"action":"route","outbound":"direct"},
      {"domain":["lan","consul"],"domain_suffix":[".lan",".consul"],"action":"route","outbound":"direct"},
      {"domain":["stale.example"],"action":"route","outbound":"direct"},
      {"ip_is_private":true,"action":"route","outbound":"direct"}
    ],
    "final": "auto"
  }
}`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func routeRules(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Route struct {
			Rules []map[string]any `json:"rules"`
		} `json:"route"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("patched config is not valid JSON: %v", err)
	}
	return got.Route.Rules
}

// The profile's own direct-bypass rules used to be dropped as "rules of the
// kind we write", and an empty Route rules panel then put nothing back — the
// organisation's process exemption and its corporate-zone rule just vanished
// on the first start.
func TestPatchKeepsProfileDirectRules(t *testing.T) {
	p := writeProfileWithDirectRules(t)
	if err := PatchSingboxRouteRules(p, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	rules := routeRules(t, p)

	var zone, process map[string]any
	for _, r := range rules {
		if _, ok := r["domain_suffix"]; ok && r["outbound"] == "direct" {
			zone = r
		}
		if _, ok := r["process_name"]; ok {
			process = r
		}
	}
	if zone == nil {
		t.Fatalf("the profile's corporate-zone rule is gone: %v", rules)
	}
	if !reflect.DeepEqual(zone["domain"], []any{"lan", "consul"}) ||
		!reflect.DeepEqual(zone["domain_suffix"], []any{".lan", ".consul"}) {
		t.Errorf("zone rule was rewritten: %v", zone)
	}
	if process == nil {
		t.Fatalf("the profile's process bypass is gone: %v", rules)
	}
	if !reflect.DeepEqual(process["process_name"], []any{"Reddy", "Reddy.exe"}) {
		t.Errorf("process names = %v, want the profile's two", process["process_name"])
	}
}

// A process the user adds joins the ones the profile ships rather than
// replacing them.
func TestPatchMergesUserProcessesWithProfile(t *testing.T) {
	p := writeProfileWithDirectRules(t)
	if err := PatchSingboxRouteRules(p, nil, []string{"Slack"}, nil); err != nil {
		t.Fatal(err)
	}
	for _, r := range routeRules(t, p) {
		if _, ok := r["process_name"]; !ok {
			continue
		}
		if !reflect.DeepEqual(r["process_name"], []any{"Reddy", "Reddy.exe", "Slack"}) {
			t.Errorf("process_name = %v", r["process_name"])
		}
		return
	}
	t.Fatal("no process rule in the patched config")
}

// Our own rule — a plain domain list, nothing else — is still replaced rather
// than accumulated, so removing a domain from the panel takes effect.
func TestPatchReplacesItsOwnDomainRule(t *testing.T) {
	p := writeProfileWithDirectRules(t)
	if err := PatchSingboxRouteRules(p, []string{"fresh.example"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	var domainOnly []map[string]any
	for _, r := range routeRules(t, p) {
		if _, hasSuffix := r["domain_suffix"]; hasSuffix {
			continue
		}
		if _, ok := r["domain"]; ok {
			domainOnly = append(domainOnly, r)
		}
	}
	if len(domainOnly) != 1 {
		t.Fatalf("expected exactly one domain-only direct rule, got %v", domainOnly)
	}
	if !reflect.DeepEqual(domainOnly[0]["domain"], []any{"fresh.example"}) {
		t.Errorf("domain rule = %v, want only the current panel entry", domainOnly[0])
	}
}
