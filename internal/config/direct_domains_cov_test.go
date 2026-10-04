package config

import (
	"strings"
	"testing"

	"jorbiquevpn/internal/jsonorder"
)

func TestParseAndValidateDirectDomains_BadLabels(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{".example.com", "empty label"},
		{"ok.lan\nexa_mple.com", "line 2: use only letters, digits and hyphens"},
		{"пример.рф", "use only letters, digits and hyphens"},
		{"-bad.com", "invalid label"},
		{"bad-.com", "invalid label"},
	} {
		if _, err := ParseAndValidateDirectDomains(tc.input); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: err = %v, want %q", tc.input, err, tc.want)
		}
	}
}

// With neither a ru-zone rule nor a private-IP rule to anchor on, the user's
// domains go after the rules already there, and entries that are not rule
// objects are stepped over.
func TestApplyUserDirectDomainSuffixes_AppendsWithoutAnAnchor(t *testing.T) {
	proxied := jsonorder.FromMap(map[string]any{"outbound": "proxy", "domain": []any{"x.example"}})
	cfg := jsonorder.FromMap(map[string]any{
		"route": jsonorder.FromMap(map[string]any{"rules": []any{"junk", proxied}}),
	})
	applyUserDirectDomainSuffixes(cfg, []string{"corp.example"})

	rules := routeRules(t, cfg)
	if len(rules) != 4 || rules[0] != "junk" || rules[1] != proxied {
		t.Fatalf("rules = %#v", rules)
	}
	domain := rules[2].(*jsonorder.Map)
	if got := field[[]any](domain, "domain"); len(got) != 1 || got[0] != "corp.example" || domain.GetString("outbound") != "direct" {
		t.Errorf("domain rule = %#v", domain)
	}
	regex := rules[3].(*jsonorder.Map)
	if got := field[[]any](regex, "domain_regex"); len(got) != 1 || got[0] != `(.*\.)?corp\.example$` {
		t.Errorf("regex rule = %#v", regex)
	}
}

// A profile that tags an outbound twice is refused, not silently half-built.
func TestFilterOutboundsAndUrltests_DuplicateTag(t *testing.T) {
	cfg := jsonorder.FromMap(map[string]any{"outbounds": []any{
		jsonorder.FromMap(map[string]any{"type": "vless", "tag": "p"}),
		jsonorder.FromMap(map[string]any{"type": "vless", "tag": "p"}),
	}})
	err := filterOutboundsAndUrltests(cfg, nil)
	if err == nil || !strings.Contains(err.Error(), `"p" is used more than once`) {
		t.Fatalf("err = %v, want a duplicate tag error", err)
	}
}
