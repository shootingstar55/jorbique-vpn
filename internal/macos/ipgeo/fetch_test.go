package ipgeo

import (
	"testing"

	"jorbiquevpn/internal/macos/core"
)

func TestLooksLikeHTML(t *testing.T) {
	if !looksLikeHTML("<html><body>503</body></html>") {
		t.Fatal("expected html")
	}
	if !looksLikeHTML("Service Unavailable") {
		t.Fatal("expected 503 text")
	}
	if looksLikeHTML("8.8.8.8") {
		t.Fatal("plain IP is not html")
	}
}

func TestParseMyIPLines(t *testing.T) {
	ip, cc, ok := parseMyIPLines("203.0.113.1\nx\nNL\n")
	if !ok || ip != "203.0.113.1" || cc != "NL" {
		t.Fatalf("got %q %q %v", ip, cc, ok)
	}
}

func TestParseJSONVariants(t *testing.T) {
	ip, cc, ok := parseJSONVariants(`{"ip":"198.51.100.2","country":"DE"}`)
	if !ok || ip != "198.51.100.2" || cc != "DE" {
		t.Fatalf("ipinfo: got %q %q %v", ip, cc, ok)
	}
	ip, cc, ok = parseJSONVariants(`{"status":"success","query":"198.51.100.3","countryCode":"CH"}`)
	if !ok || ip != "198.51.100.3" || cc != "CH" {
		t.Fatalf("ip-api: got %q %q %v", ip, cc, ok)
	}
	ip, cc, ok = parseJSONVariants(`{"ip":"198.51.100.4"}`)
	if !ok || ip != "198.51.100.4" || cc != "" {
		t.Fatalf("ipify: got %q %q %v", ip, cc, ok)
	}
	ip, cc, ok = parseJSONVariants(`{"origin": "198.51.100.5, 1.1.1.1"}`)
	if !ok || ip != "198.51.100.5" || cc != "" {
		t.Fatalf("httpbin origin: got %q %q %v", ip, cc, ok)
	}
	ip, cc, ok = parseJSONVariants(`{"ip": "1.0.0.0", "country_iso": "US"}`)
	if !ok || ip != "1.0.0.0" || cc != "US" {
		t.Fatalf("country_iso: got %q %q %v", ip, cc, ok)
	}
}

func TestParseCloudflareTrace(t *testing.T) {
	ip, ok := parseCloudflareTrace("fl=foo\nip=198.51.100.5\n")
	if !ok || ip != "198.51.100.5" {
		t.Fatalf("got %q %v", ip, ok)
	}
}

func TestIsValidIPStringRejectsJunk(t *testing.T) {
	if isValidIPString(`<html><body>503`) {
		t.Fatal("html should be rejected")
	}
	if !isValidIPString("1.1.1.1") {
		t.Fatal("valid ipv4")
	}
}

func TestBuildURLChainDedupes(t *testing.T) {
	a := buildURLChain(core.EffectiveAPI{
		IPUrl:  "https://ipinfo.io/json",
		GeoUrl: "https://ipinfo.io/json",
	})
	if len(a) < 3 {
		t.Fatalf("expected multiple URLs, got %d", len(a))
	}
	seen := map[string]int{}
	for _, u := range a {
		seen[u]++
	}
	for u, n := range seen {
		if n > 1 {
			t.Fatalf("duplicate url %q", u)
		}
	}
}
