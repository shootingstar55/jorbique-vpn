package config

import (
	"testing"

	"jorbiquevpn/internal/jsonorder"
)

// baseRouteCfg returns a config skeleton with a route.rules array resembling the
// embedded template: a DNS rule, a source_ip_cidr direct rule, a private-IP
// direct rule, and a ru-zone domain_regex direct rule.
func baseRouteCfg() *jsonorder.Map {
	return jsonorder.FromMap(map[string]any{
		"route": jsonorder.FromMap(map[string]any{
			"rules": []any{
				jsonorder.FromMap(map[string]any{"protocol": "dns", "action": "hijack-dns"}),
				jsonorder.FromMap(map[string]any{"outbound": "direct", "action": "route", "source_ip_cidr": []any{"10.8.0.0/16"}}),
				jsonorder.FromMap(map[string]any{"outbound": "direct", "action": "route", "ip_is_private": true}),
				jsonorder.FromMap(map[string]any{"outbound": "direct", "action": "route", "domain_regex": []any{`.*\.ru$`}}),
			},
		}),
	})
}

func routeRules(t *testing.T, cfg *jsonorder.Map) []any {
	t.Helper()
	route, ok := mustField[*jsonorder.Map](cfg, "route")
	if !ok {
		t.Fatalf("route missing: %#v", cfg)
	}
	rules, ok := mustField[[]any](route, "rules")
	if !ok {
		t.Fatalf("route.rules missing: %#v", route)
	}
	return rules
}

// findRuleWithKey returns the first rule map that has a non-nil value for key.
func findRuleWithKey(rules []any, key string) *jsonorder.Map {
	for _, r := range rules {
		m, ok := r.(*jsonorder.Map)
		if ok && m.Has(key) {
			return m
		}
	}
	return nil
}

func TestApplyIgnoredProcesses_InsertsRuleAfterOpenVpnCidr(t *testing.T) {
	cfg := baseRouteCfg()
	applyIgnoredProcesses(cfg, []string{"chrome", "Game.exe", "", "chrome"})

	rules := routeRules(t, cfg)
	rule := findRuleWithKey(rules, "process_name")
	if rule == nil {
		t.Fatalf("no process_name rule inserted: %#v", rules)
	}
	names, _ := mustField[[]any](rule, "process_name")
	got := map[string]bool{}
	for _, n := range names {
		got[n.(string)] = true
	}
	// "chrome" expands to chrome + chrome.exe; "Game.exe" expands to Game.exe + Game.
	for _, want := range []string{"chrome", "chrome.exe", "Game.exe", "Game"} {
		if !got[want] {
			t.Errorf("expected expanded name %q in %v", want, got)
		}
	}
	if field[any](rule, "outbound") != "direct" {
		t.Errorf("inserted rule outbound = %v, want direct", field[any](rule, "outbound"))
	}
}

func TestApplyIgnoredProcesses_MergesIntoExistingProcessNameRule(t *testing.T) {
	cfg := jsonorder.FromMap(map[string]any{
		"route": jsonorder.FromMap(map[string]any{
			"rules": []any{
				jsonorder.FromMap(map[string]any{"outbound": "direct", "process_name": []any{"existing.exe"}}),
			},
		}),
	})
	applyIgnoredProcesses(cfg, []string{"new", "existing.exe"})

	rule := findRuleWithKey(routeRules(t, cfg), "process_name")
	names, _ := mustField[[]any](rule, "process_name")
	count := map[string]int{}
	for _, n := range names {
		count[n.(string)]++
	}
	if count["existing.exe"] != 1 {
		t.Errorf("existing.exe duplicated: %v", count)
	}
	if count["new"] == 0 || count["new.exe"] == 0 {
		t.Errorf("new name not merged with expansion: %v", count)
	}
}

func TestApplyIgnoredProcesses_NoopOnEmptyOrMissingRoute(t *testing.T) {
	cfg := baseRouteCfg()
	before := len(routeRules(t, cfg))
	applyIgnoredProcesses(cfg, nil)
	if len(routeRules(t, cfg)) != before {
		t.Error("empty ignore list should not add rules")
	}
	applyIgnoredProcesses(nil, []string{"x"}) // must not panic
	applyIgnoredProcesses(jsonorder.FromMap(map[string]any{}), []string{"x"})
	// Whitespace-only names produce no rule.
	cfg2 := baseRouteCfg()
	applyIgnoredProcesses(cfg2, []string{"   ", ""})
	if findRuleWithKey(routeRules(t, cfg2), "process_name") != nil {
		t.Error("whitespace-only names should not insert a rule")
	}
}

func TestApplyIgnoredProcessPaths_InsertsAndMerges(t *testing.T) {
	cfg := baseRouteCfg()
	applyIgnoredProcessPaths(cfg, []string{"/usr/bin/app", "relative/not/abs", "", "/usr/bin/app"})
	rule := findRuleWithKey(routeRules(t, cfg), "process_path")
	if rule == nil {
		t.Fatalf("no process_path rule inserted")
	}
	paths, _ := mustField[[]any](rule, "process_path")
	if len(paths) != 1 || paths[0].(string) != "/usr/bin/app" {
		t.Errorf("expected single cleaned absolute path, got %v", paths)
	}

	// Merge into an existing process_path direct rule.
	cfg2 := jsonorder.FromMap(map[string]any{
		"route": jsonorder.FromMap(map[string]any{
			"rules": []any{
				jsonorder.FromMap(map[string]any{"outbound": "direct", "process_path": []any{"/opt/keep"}}),
			},
		}),
	})
	applyIgnoredProcessPaths(cfg2, []string{"/opt/keep", "/opt/new"})
	merged := findRuleWithKey(routeRules(t, cfg2), "process_path")
	vals, _ := mustField[[]any](merged, "process_path")
	if len(vals) != 2 {
		t.Errorf("expected merge to 2 paths, got %v", vals)
	}
}

func TestApplyIgnoredProcessPaths_NoopCases(t *testing.T) {
	cfg := baseRouteCfg()
	before := len(routeRules(t, cfg))
	applyIgnoredProcessPaths(cfg, nil)
	applyIgnoredProcessPaths(cfg, []string{"relative/only", ""})
	if len(routeRules(t, cfg)) != before {
		t.Error("no absolute paths should not add a rule")
	}
	applyIgnoredProcessPaths(nil, []string{"/x"}) // must not panic
}

func TestApplyUserDirectDomainSuffixes_AddsDomainAndMergesRegex(t *testing.T) {
	cfg := baseRouteCfg()
	applyUserDirectDomainSuffixes(cfg, []string{"example.com", "", "  "})
	rules := routeRules(t, cfg)

	domainRule := findRuleWithKey(rules, "domain")
	if domainRule == nil {
		t.Fatalf("no domain direct rule added: %#v", rules)
	}
	doms, _ := mustField[[]any](domainRule, "domain")
	if len(doms) != 1 || doms[0].(string) != "example.com" {
		t.Errorf("domain rule = %v, want [example.com]", doms)
	}

	// Regex merged into the existing ru-zone domain_regex rule.
	regexRule := findRuleWithKey(rules, "domain_regex")
	rx, _ := mustField[[]any](regexRule, "domain_regex")
	var hasExample bool
	for _, v := range rx {
		if v.(string) == `(.*\.)?example\.com$` {
			hasExample = true
		}
	}
	if !hasExample {
		t.Errorf("escaped regex for example.com not merged: %v", rx)
	}
}

func TestApplyUserDirectDomainSuffixes_NoRuZoneAppendsRegexRule(t *testing.T) {
	cfg := jsonorder.FromMap(map[string]any{
		"route": jsonorder.FromMap(map[string]any{
			"rules": []any{
				jsonorder.FromMap(map[string]any{"outbound": "direct", "ip_is_private": true}),
			},
		}),
	})
	applyUserDirectDomainSuffixes(cfg, []string{"corp.local"})
	rules := routeRules(t, cfg)
	regexRule := findRuleWithKey(rules, "domain_regex")
	if regexRule == nil {
		t.Fatalf("expected appended domain_regex rule when no ru-zone rule exists: %#v", rules)
	}
	rx, _ := mustField[[]any](regexRule, "domain_regex")
	if len(rx) != 1 || rx[0].(string) != `(.*\.)?corp\.local$` {
		t.Errorf("appended regex = %v", rx)
	}
}

func TestApplyUserDirectDomainSuffixes_NoopCases(t *testing.T) {
	cfg := baseRouteCfg()
	before := len(routeRules(t, cfg))
	applyUserDirectDomainSuffixes(cfg, nil)
	applyUserDirectDomainSuffixes(cfg, []string{"", "  "})
	if len(routeRules(t, cfg)) != before {
		t.Error("blank domains should not change rules")
	}
	applyUserDirectDomainSuffixes(nil, []string{"x.com"}) // must not panic
}

func TestIsRuZoneDirectRule(t *testing.T) {
	cases := []struct {
		name string
		rule *jsonorder.Map
		want bool
	}{
		{"legacy combined", jsonorder.FromMap(map[string]any{"outbound": "direct", "domain_regex": []any{`.*\.(ru|su|xn--p1ai)$`}}), true},
		{"ru only", jsonorder.FromMap(map[string]any{"outbound": "direct", "domain_regex": []any{`.*\.ru$`}}), true},
		{"not direct", jsonorder.FromMap(map[string]any{"outbound": "proxy", "domain_regex": []any{`.*\.ru$`}}), false},
		{"no domain_regex", jsonorder.FromMap(map[string]any{"outbound": "direct"}), false},
		{"unrelated regex", jsonorder.FromMap(map[string]any{"outbound": "direct", "domain_regex": []any{`.*\.com$`}}), false},
		{"nil map", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isRuZoneDirectRule(c.rule); got != c.want {
				t.Errorf("isRuZoneDirectRule = %v, want %v", got, c.want)
			}
		})
	}
}

func TestMergeRegexInterfacesAndStringSlice(t *testing.T) {
	existing := []any{"a", "a", 5} // dup + non-string ignored
	out := mergeRegexInterfaces(existing, []string{"a", "b"})
	if len(out) != 2 || out[0] != "a" || out[1] != "b" {
		t.Errorf("mergeRegexInterfaces = %v, want [a b]", out)
	}
	out = mergeRegexInterfaces(nil, []string{"x", "x", "y"})
	if len(out) != 2 {
		t.Errorf("dedup failed: %v", out)
	}

	si := stringSliceToInterface([]string{"p", "q"})
	if len(si) != 2 || si[0] != "p" || si[1] != "q" {
		t.Errorf("stringSliceToInterface = %v", si)
	}
	if len(stringSliceToInterface(nil)) != 0 {
		t.Error("nil slice should yield empty")
	}
}

func TestBypassRuleInsertIndex(t *testing.T) {
	// After the source_ip_cidr direct rule (index 1) -> insert at 2.
	if got := bypassRuleInsertIndex(routeRules(t, baseRouteCfg())); got != 2 {
		t.Errorf("insert index = %d, want 2 (after source_ip_cidr rule)", got)
	}
	// No CIDR rule: insert after last dns rule.
	rules := []any{
		jsonorder.FromMap(map[string]any{"protocol": "dns"}),
		jsonorder.FromMap(map[string]any{"protocol": "dns"}),
		jsonorder.FromMap(map[string]any{"outbound": "direct"}),
	}
	if got := bypassRuleInsertIndex(rules); got != 2 {
		t.Errorf("insert index = %d, want 2 (after last dns rule)", got)
	}
	// Neither CIDR nor dns: insert at 0.
	if got := bypassRuleInsertIndex([]any{jsonorder.FromMap(map[string]any{"outbound": "direct"})}); got != 0 {
		t.Errorf("insert index = %d, want 0", got)
	}
}

// Routing a name direct is only half of it: the lookup still went through the
// tunnel to the profile's DoH server, so a host that answers only on the local
// network never resolved. The same names go to the profile's local resolver.
func TestDirectDomainsAlsoResolveLocally(t *testing.T) {
	cfg := jsonorder.FromMap(map[string]any{
		"dns": jsonorder.FromMap(map[string]any{
			"servers": []any{
				jsonorder.FromMap(map[string]any{"tag": "doh-cloudflare", "type": "https"}),
				jsonorder.FromMap(map[string]any{"tag": "dns-local", "type": "local"}),
			},
			"rules": []any{jsonorder.FromMap(map[string]any{"domain": []any{"lan"}, "server": "dns-remote"})},
		}),
		"route": jsonorder.FromMap(map[string]any{"rules": []any{
			jsonorder.FromMap(map[string]any{"outbound": "direct", "ip_is_private": true}),
		}}),
	})
	applyUserDirectDomainSuffixes(cfg, []string{"example.com"})

	dns, _ := mustField[*jsonorder.Map](cfg, "dns")
	rules, _ := mustField[[]any](dns, "rules")
	first, ok := rules[0].(*jsonorder.Map)
	if !ok {
		t.Fatalf("first dns rule = %#v", rules[0])
	}
	// First, because the first matching rule wins and this is the user's answer.
	if got := first.GetString("server"); got != "dns-local" {
		t.Errorf("direct names resolve through %q, want the local server", got)
	}
	domains, _ := mustField[[]any](first, "domain")
	suffixes, _ := mustField[[]any](first, "domain_suffix")
	if len(domains) != 1 || domains[0] != "example.com" || len(suffixes) != 1 || suffixes[0] != ".example.com" {
		t.Errorf("rule covers %v / %v", domains, suffixes)
	}
	if len(rules) != 2 {
		t.Errorf("the profile's own rule was lost: %d rules", len(rules))
	}
}

// A profile with no local server has nothing to point them at, and inventing a
// server tag would be a config sing-box refuses to start.
func TestDirectDomainsWithoutALocalResolverChangeNoDNS(t *testing.T) {
	cfg := jsonorder.FromMap(map[string]any{
		"dns": jsonorder.FromMap(map[string]any{
			"servers": []any{jsonorder.FromMap(map[string]any{"tag": "doh-cloudflare", "type": "https"})},
			"rules":   []any{},
		}),
		"route": jsonorder.FromMap(map[string]any{"rules": []any{
			jsonorder.FromMap(map[string]any{"outbound": "direct", "ip_is_private": true}),
		}}),
	})
	applyUserDirectDomainSuffixes(cfg, []string{"example.com"})

	dns, _ := mustField[*jsonorder.Map](cfg, "dns")
	if rules, _ := mustField[[]any](dns, "rules"); len(rules) != 0 {
		t.Errorf("a dns rule was added with no local server to point at: %v", rules)
	}
}
