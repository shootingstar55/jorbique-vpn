package singbox

import (
	"jorbiquevpn/internal/macos/core"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The profile shape the server serves now: a selector over the urltest, with the
// resolver reaching out through it.
const selectorProfileJSON = `{
  "dns": {"servers": [
    {"tag": "doh-cloudflare", "type": "https", "server": "1.1.1.1", "detour": "proxy-selector"},
    {"tag": "dns-local", "type": "local"}
  ], "final": "doh-cloudflare"},
  "outbounds": [
    {"type": "vless", "tag": "vless-reality", "server": "example.org", "server_port": 443, "uuid": ""},
    {"type": "tuic", "tag": "tuic", "server": "example.org", "server_port": 443, "uuid": "", "password": "x"},
    {"type": "urltest", "tag": "auto", "outbounds": ["vless-reality", "tuic"]},
    {"type": "selector", "tag": "proxy-selector", "outbounds": ["auto", "vless-reality", "tuic"], "default": "auto"},
    {"type": "direct", "tag": "direct"}
  ],
  "route": {"default_domain_resolver": "doh-cloudflare", "rules": [], "final": "proxy-selector"}
}`

func buildFromProfile(t *testing.T, profile string, disabled []string, node string) *OrderedMap {
	t.Helper()
	dir := t.TempDir()
	base := filepath.Join(dir, "singbox.json")
	if err := os.WriteFile(base, []byte(`{"log":{"level":"warn"},"inbounds":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "singbox_client.json"), []byte(profile), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := BuildConfigFromRemoteTemplate(core.SingboxSection{UUID: "11111111-1111-1111-1111-111111111111", DisabledOutbounds: disabled, SelectedNode: node}, base, time.Nanosecond, nil)
	if err != nil {
		t.Fatalf("BuildConfigFromRemoteTemplate: %v", err)
	}
	return root
}

func outboundByType(t *testing.T, root *OrderedMap, typ string) *OrderedMap {
	t.Helper()
	raw, _ := root.Get("outbounds")
	list, _ := raw.([]any)
	for _, o := range list {
		if m, ok := o.(*OrderedMap); ok && m.GetString("type") == typ {
			return m
		}
	}
	return nil
}

// The selector is plumbing, not a protocol a user switches off. Dropping it took
// the DNS with it — the resolver detours through the selector, so pruning
// removed the resolver, and sing-box then refused the config over an outbound
// naming a resolver that was no longer there.
func TestTheSelectorSurvivesTheBuild(t *testing.T) {
	root := buildFromProfile(t, selectorProfileJSON, nil, "")

	sel := outboundByType(t, root, "selector")
	if sel == nil {
		t.Fatal("the built config has no selector")
	}
	if got := sel.GetString("tag"); got != "proxy-selector" {
		t.Errorf("selector tag = %q", got)
	}

	dns, _ := root.Get("dns")
	servers, _ := dns.(*OrderedMap).Get("servers")
	if len(servers.([]any)) != 2 {
		t.Errorf("the resolver that detours through the selector was pruned: %v", servers)
	}
}

func TestThePickedNodeBecomesTheSelectorDefault(t *testing.T) {
	root := buildFromProfile(t, selectorProfileJSON, nil, "tuic")
	if got := outboundByType(t, root, "selector").GetString("default"); got != "tuic" {
		t.Errorf("default = %q, want tuic", got)
	}

	// A node switched off since it was picked leaves the profile's own default
	// standing: a "default" naming a tag that is not there costs the whole
	// tunnel, not just that node.
	root = buildFromProfile(t, selectorProfileJSON, []string{"tuic"}, "tuic")
	sel := outboundByType(t, root, "selector")
	if got := sel.GetString("default"); got != "auto" {
		t.Errorf("default after the node was switched off = %q, want auto", got)
	}
	list, _ := sel.Get("outbounds")
	for _, v := range list.([]any) {
		if v == "tuic" {
			t.Error("a node that was switched off is still offered by the selector")
		}
	}
}
