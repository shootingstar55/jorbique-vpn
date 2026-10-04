package ipgeo

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"jorbiquevpn/internal/macos/core"
)

// IPv6 answers keep the plain-IP paths off the network: countryForIPv4 only
// looks up a country for IPv4.
func TestTryOneURLWithGetter(t *testing.T) {
	for _, tc := range []struct {
		name, url string
		status    int
		body      string
		ip, cc    string
		ok        bool
	}{
		{name: "empty url", url: " "},
		{name: "getter failed", url: "u", status: 1, body: "203.0.113.1"},
		{name: "empty body", url: "u"},
		{name: "html error page", url: "u", body: "<html>503</html>"},
		{name: "json", url: "u", body: `{"ip":"203.0.113.2","country":" NL "}`, ip: "203.0.113.2", cc: "NL", ok: true},
		{name: "my-ip lines", url: "u", body: "2001:db8::2\nv6\n DE \n", ip: "2001:db8::2", cc: "DE", ok: true},
		{name: "cloudflare trace", url: "u", body: "fl=1f1\nip=2001:db8::3\nloc=NL", ip: "2001:db8::3", ok: true},
		{name: "garbage", url: "u", body: "not an ip\nat all"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var asked string
			get := func(u string) (int, string) { asked = u; return tc.status, tc.body }
			ip, cc, ok := tryOneURLWithGetter(" "+tc.url+" ", get)
			if ip != tc.ip || cc != tc.cc || ok != tc.ok {
				t.Fatalf("got (%q, %q, %v), want (%q, %q, %v)", ip, cc, ok, tc.ip, tc.cc, tc.ok)
			}
			if tc.url != " " && asked != tc.url {
				t.Fatalf("getter asked for %q, want trimmed %q", asked, tc.url)
			}
		})
	}
}

func TestWithIPv4CountrySkipsLookupForIPv6(t *testing.T) {
	ip, cc, ok := withIPv4Country("2001:db8::4")
	if ip != "2001:db8::4" || cc != "" || !ok {
		t.Fatalf("got (%q, %q, %v)", ip, cc, ok)
	}
}

func TestExtendStreak(t *testing.T) {
	if ip, n := extendStreak("", 0, "203.0.113.5"); ip != "203.0.113.5" || n != 1 {
		t.Fatalf("new streak: got %q %d", ip, n)
	}
	if ip, n := extendStreak("203.0.113.5", 1, "203.0.113.5"); ip != "203.0.113.5" || n != 2 {
		t.Fatalf("same ip: got %q %d", ip, n)
	}
	if ip, n := extendStreak("203.0.113.5", 3, "203.0.113.6"); ip != "203.0.113.6" || n != 1 {
		t.Fatalf("changed ip: got %q %d", ip, n)
	}
}

func TestWaitStableReturnsFirstStableAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ip":"203.0.113.9","country":"CH"}`))
	}))
	defer srv.Close()
	saved := defaultFallbacks
	defaultFallbacks = nil // never fall through to the public services
	t.Cleanup(func() { defaultFallbacks = saved })

	ip, cc, ok := WaitStable(core.EffectiveAPI{IPUrl: srv.URL}, WaitOpts{StableNeeded: 1})
	if ip != "203.0.113.9" || cc != "CH" || !ok {
		t.Fatalf("got (%q, %q, %v)", ip, cc, ok)
	}
}
