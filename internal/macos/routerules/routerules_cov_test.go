package routerules

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// A vpn_cfg.json that does not parse is reported, not rewritten.
func TestPatchVpnCfgRouteRulesRefusesInvalidJSON(t *testing.T) {
	const broken = `{"direct_domains": [`
	p := writeCfg(t, broken)
	err := PatchVpnCfgRouteRules(p, []string{"example.com"}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid vpn_cfg.json") {
		t.Fatalf("err = %v, want an invalid vpn_cfg.json error", err)
	}
	if data, _ := os.ReadFile(p); string(data) != broken {
		t.Errorf("the broken file was rewritten: %q", data)
	}
}

// A config with no rule arrays has nothing to patch, and is left byte for byte.
func TestPatchSingboxRouteRulesWithoutRuleArrays(t *testing.T) {
	const body = `{
  "dns": {"servers": []},
  "route": {"final": "direct"}
}`
	p := writeCfg(t, body)
	if err := PatchSingboxRouteRules(p, []string{"example.com"}, []string{"Slack"}, nil); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(p); string(data) != body {
		t.Errorf("a config without rules was changed:\n%s", data)
	}
}

// The text splice looks for the first "route" object. When that one is nested
// and carries no rules, the route section is left alone rather than spliced
// into the wrong place; the dns section is still patched.
func TestPatchSingboxRouteRulesSkipsASectionItCannotLocate(t *testing.T) {
	const body = `{
  "experimental": {"route": {"note": "not the top-level one"}},
  "dns": {"servers": [{"tag": "dns-local", "type": "local"}], "rules": []},
  "route": {"rules": [{"outbound": "direct", "domain": ["old.example"]}]}
}`
	p := writeCfg(t, body)
	if err := PatchSingboxRouteRules(p, []string{"example.com"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	if !strings.Contains(string(data), `"route": {"rules": [{"outbound": "direct", "domain": ["old.example"]}]}`) {
		t.Errorf("the route section was touched:\n%s", data)
	}
	if !strings.Contains(string(data), `"server": "dns-local"`) {
		t.Errorf("the dns rules were not written:\n%s", data)
	}
	mustStillParse(t, p)
}

// A direct domain joins the profile's .ru regex rule instead of adding a second
// regex rule, and a profile direct rule missing its action gains one.
func TestPatchSingboxRouteRulesMergesIntoTheRuRegexRule(t *testing.T) {
	const body = `{
  "route": {
    "rules": [
      {"outbound": "direct", "domain_regex": [".*\\.ru$"]},
      {"outbound": "direct", "domain": ["corp"], "domain_suffix": [".corp"]}
    ]
  }
}`
	p := writeCfg(t, body)
	if err := PatchSingboxRouteRules(p, []string{"example.com"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	var regexRules []map[string]any
	var corp map[string]any
	for _, r := range routeRules(t, p) {
		if _, ok := r["domain_regex"]; ok {
			regexRules = append(regexRules, r)
		}
		if _, ok := r["domain_suffix"]; ok {
			corp = r
		}
	}
	if len(regexRules) != 1 {
		t.Fatalf("regex rules = %v, want the profile's one only", regexRules)
	}
	want := []any{`.*\.ru$`, `(.*\.)?example\.com$`}
	if got := regexRules[0]["domain_regex"]; !reflect.DeepEqual(got, want) {
		t.Errorf("domain_regex = %v, want %v", got, want)
	}
	if corp == nil || corp["action"] != "route" {
		t.Errorf("corp rule = %v, want action route added", corp)
	}
}

func TestJSONContainerEndCov(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		want       int
	}{
		{"escaped quote and bracket inside a string", `["a\"]", 1] tail`, len(`["a\"]", 1]`)},
		{"unterminated string", `["abc`, -1},
		{"unterminated array", `[1, 2`, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := jsonContainerEnd(tc.text, 0, '[', ']'); got != tc.want {
				t.Errorf("jsonContainerEnd(%q) = %d, want %d", tc.text, got, tc.want)
			}
		})
	}
}

func TestFindRulesArrayRangeCov(t *testing.T) {
	// The object closes but the rules array inside it never does.
	if s, e := findRulesArrayRangeInTopLevelSection(`{"route": {"rules": [ }`, "route"); s != -1 || e != -1 {
		t.Errorf("unterminated rules = (%d, %d), want (-1, -1)", s, e)
	}
	text := `{"custom": {"rules": [1, 2]}}`
	s, e := findRulesArrayRangeInTopLevelSection(text, "custom")
	if s < 0 || text[s:e] != "[1, 2]" {
		t.Errorf("custom section rules = (%d, %d)", s, e)
	}
}

// A selected_node already in the file is rewritten in place, not added twice.
func TestPatchVpnCfgReplacesAnExistingSelectedNode(t *testing.T) {
	p := writeCfg(t, `{
  "singbox": {
    "uuid": "",
    "enabled_outbounds": [],
    "disabled_outbounds": [],
    "selected_node": "old"
  }
}`)
	node := "vless-reality"
	if err := PatchVpnCfgSingboxUUIDAndOutbounds(p, nil, nil, nil, &node); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	if n := strings.Count(string(data), `"selected_node"`); n != 1 {
		t.Errorf("selected_node appears %d times:\n%s", n, data)
	}
	sb, _ := mustStillParse(t, p)["singbox"].(map[string]any)
	if sb["selected_node"] != node {
		t.Errorf("selected_node = %v, want %q", sb["selected_node"], node)
	}
}

// With none of the choice lists to sit after, there is nowhere to put the key.
func TestEnsureSelectedNodeKeyWithoutAnchor(t *testing.T) {
	const content = `{"singbox": {}}`
	if got := ensureSelectedNodeKey(content); got != content {
		t.Errorf("ensureSelectedNodeKey = %q, want it unchanged", got)
	}
}

// An array that never closes is not one to overwrite.
func TestReplaceJSONArrayOfStringsUnterminated(t *testing.T) {
	const content = `{"enabled_outbounds": ["a", "b"`
	if got := replaceJSONArrayOfStrings("enabled_outbounds", []string{"c"}, content); got != content {
		t.Errorf("replaceJSONArrayOfStrings = %q, want it unchanged", got)
	}
}
