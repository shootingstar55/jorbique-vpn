// Package ipgeo resolves public exit IP and country with multiple fallbacks and strict validation
// (avoids showing HTML 503 pages as "IP" when a provider is down).
package ipgeo

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/webprobe"
)

// probeTimeout is the whole request, as curl's --max-time 8 was.
const probeTimeout = 8 * time.Second

// defaultFallbacks are tried after user-defined api.ip_url, api.ip_urls, and api.geo_url (if any).
// Each entry is hit with curl --fail so HTTP 4xx/5xx and HTML error pages are skipped.
// List combines JSON (ip + optional country) and plain-text / Cloudflare-trace services;
// some operators rate-limit; order + dedup in buildURLChain keeps the first success.
// api.myip.com is listed first: same host family as the myip.com page many users check in a browser
// (per-domain rules in the tunnel then match what you are comparing to).
var defaultFallbacks = []string{
	// JSON (parseJSONVariants)
	"https://api.myip.com",
	"https://get.geojs.io/v1/ip.json",
	"https://ipinfo.io/json",
	"https://ifconfig.io/all.json",
	"https://ifconfig.co/json",
	"https://ip-api.com/json/?fields=status,message,query,countryCode",
	"https://ipwho.is/",
	"https://ipapi.co/json/",
	"https://v4.api.ipify.org?format=json",
	"https://api.ipify.org?format=json",
	"https://api64.ipify.org?format=json",
	"https://jsonip.com",
	"https://httpbin.org/ip",
	// Cloudflare trace (ip=)
	"https://www.cloudflare.com/cdn-cgi/trace",
	"https://1.0.0.1/cdn-cgi/trace",
	"https://1.1.1.1/cdn-cgi/trace",
	// my-ip: popular default in configs; 503s are skipped by curl -f, later URLs still work
	"https://api.my-ip.io/v2/ip.txt",
	// Plain first-line IPv4
	"https://ifconfig.me/ip",
	"https://ifconfig.io/ip",
	"https://4.ifconfig.me/ip",
	"https://4.icanhazip.com",
	"https://icanhazip.com",
	"https://ident.me",
	"https://ipecho.net/plain",
	"https://l2.io/ip",
	"https://checkip.amazonaws.com",
	"https://myexternalip.com/raw",
	"https://tnx.nl/ip",
	"https://whatismyip.akamai.com",
	"https://wgetip.com",
	"https://ip.tyk.nu",
	"https://4.ident.me",
	"https://4.ifconfig.io/ip",
	"https://api.ipify.org",
}

// Fetch returns the best-effort public IPv4 and optional ISO country code using cfg.Api URLs first,
// then built-in services. On total failure, returns "", "".
// The primary attempt uses only the effective ip_url and parses whatever comes
// back; the fallbacks are stricter and skip anything that answers 4xx or 5xx.
func Fetch(api core.EffectiveAPI) (ip, country string) {
	if i, c, ok := tryOneURLWithGetter(api.IPUrl, fetchPrimary); ok && isValidIPString(i) {
		return i, c
	}
	chain := buildURLChain(api)
	for _, u := range chain {
		if i, c, ok := tryOneURL(u); ok && isValidIPString(i) {
			return i, c
		}
	}
	return "", ""
}

func buildURLChain(api core.EffectiveAPI) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	add(api.IPUrl)
	for _, u := range api.ExtraIPUrls {
		add(u)
	}
	add(api.GeoUrl)
	for _, u := range defaultFallbacks {
		add(u)
	}
	return out
}

func tryOneURL(findURL string) (ip, cc string, ok bool) {
	return tryOneURLWithGetter(findURL, fetchFallback)
}

func tryOneURLWithGetter(findURL string, get func(string) (int, string)) (ip, cc string, ok bool) {
	findURL = strings.TrimSpace(findURL)
	if findURL == "" {
		return "", "", false
	}
	st, body := get(findURL)
	if st != 0 || body == "" {
		return "", "", false
	}
	body = strings.TrimSpace(body)
	if looksLikeHTML(body) {
		return "", "", false
	}
	// JSON: geo endpoints or ipify
	if strings.HasPrefix(body, "{") {
		if ip, cc, okp := parseJSONVariants(body); okp && isValidIPString(ip) {
			return ip, strings.TrimSpace(cc), true
		}
	}
	// my-ip.io: line1=ip, line3=country
	if ip, cc, okm := parseMyIPLines(body); okm && isValidIPString(ip) {
		return ip, strings.TrimSpace(cc), true
	}
	// Cloudflare trace: ip=...
	if ip, okc := parseCloudflareTrace(body); okc && isValidIPString(ip) {
		return withIPv4Country(ip)
	}
	// single-line IP
	if ip, okl := firstLineIP(body); okl && isValidIPString(ip) {
		return withIPv4Country(ip)
	}
	return "", "", false
}

// withIPv4Country pairs a plain IP with its country when the lookup finds one;
// the IP alone is still a success.
func withIPv4Country(ip string) (string, string, bool) {
	if c2, ok2 := countryForIPv4(ip); ok2 {
		return ip, c2, true
	}
	return ip, "", true
}

// fetchPrimary reads the user's own ip_url. Whatever it answers is parsed, the
// way the first curl did: no --fail, because a service that returns a 200 page
// of nonsense and one that returns a 500 are both handled by the parsers below.
func fetchPrimary(u string) (int, string) {
	res, err := webprobe.Get(context.Background(), u, webprobe.Options{
		IPv4Only: true,
		Timeout:  probeTimeout,
	})
	if err != nil {
		return exitCodeForError(err), ""
	}
	return 0, res.Body
}

// fetchFallback is the same request with curl's --fail: a 4xx or 5xx from one
// of the public services means try the next one, not parse an error page.
func fetchFallback(u string) (int, string) {
	res, err := webprobe.Get(context.Background(), u, webprobe.Options{
		IPv4Only:        true,
		FailOnHTTPError: true,
		Timeout:         probeTimeout,
	})
	if err != nil {
		return exitCodeForError(err), ""
	}
	return 0, res.Body
}

// exitCodeForError keeps the callers' "non-zero means it did not work" shape.
// The number itself is no longer curl's, and nothing reads it beyond that.
func exitCodeForError(err error) int {
	if err == nil {
		return 0
	}
	return 1
}

func looksLikeHTML(s string) bool {
	low := strings.ToLower(s)
	if strings.Contains(low, "<html") || strings.Contains(low, "<!doctype") {
		return true
	}
	if strings.Contains(low, "service unavailable") {
		return true
	}
	return false
}

func isValidIPString(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	// Reject if it could not be an IP (e.g. HTML fragment still slipped through)
	if strings.ContainsAny(s, "<>&") {
		return false
	}
	return net.ParseIP(s) != nil
}

func parseMyIPLines(body string) (ip, cc string, ok bool) {
	lines := strings.Split(body, "\n")
	if len(lines) < 1 {
		return "", "", false
	}
	ip = strings.TrimSpace(lines[0])
	if len(lines) >= 3 {
		cc = strings.TrimSpace(lines[2])
	}
	if ip == "" {
		return "", "", false
	}
	return ip, cc, true
}

func parseCloudflareTrace(body string) (ip string, ok bool) {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ip=") {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(line, "ip="))
		if v != "" {
			return v, true
		}
	}
	return "", false
}

func firstLineIP(body string) (string, bool) {
	for _, line := range strings.Split(body, "\n") {
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}
		if isValidIPString(s) {
			return s, true
		}
		break
	}
	return "", false
}

// parseJSONVariants handles ipinfo, ip-api, ipify, and loose {"ip", "country" | "countryCode"}.
func parseJSONVariants(body string) (ip, cc string, ok bool) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		return "", "", false
	}
	// ipify: {"ip":"x"}
	// ipinfo: {"ip","country" (2-letter)}
	// ip-api: {"query","countryCode","status"}
	// my-ip? etc.
	ip = firstJSONString(m, "ip", "query", "origin")
	// e.g. httpbin when behind multiple proxies: "1.1.1.1, 2.2.2.2"
	if strings.Contains(ip, ",") {
		ip = strings.TrimSpace(strings.Split(ip, ",")[0])
	}
	if st, has := m["status"]; has {
		var status string
		_ = json.Unmarshal(st, &status)
		if status != "" && !strings.EqualFold(status, "success") {
			return "", "", false
		}
	}
	cc = firstJSONString(m, "country", "countryCode", "country_code", "country_iso", "cc")
	if ip == "" {
		return "", "", false
	}
	ip = strings.TrimSpace(ip)
	if !isValidIPString(ip) {
		return "", "", false
	}
	cc = strings.TrimSpace(cc)
	return ip, cc, true
}

// firstJSONString returns the first of keys whose value is a non-empty JSON
// string, or "" when none is.
func firstJSONString(m map[string]json.RawMessage, keys ...string) string {
	for _, k := range keys {
		if raw, ok1 := m[k]; ok1 {
			var s string
			if err := json.Unmarshal(raw, &s); err == nil && s != "" {
				return s
			}
		}
	}
	return ""
}

// countryForIPv4 fetches a small ipinfo json for a known IPv4 when we only had plain IP.
func countryForIPv4(ipv4 string) (cc string, ok bool) {
	parsed := net.ParseIP(ipv4)
	if parsed == nil {
		return "", false
	}
	// If not IPv4, path may need encoding; we only use this after -4 curl plain IP, so expect IPv4.
	if parsed.To4() == nil {
		return "", false
	}
	u := fmt.Sprintf("https://ipinfo.io/%s/json", url.PathEscape(ipv4))
	st, body := fetchFallback(u)
	if st != 0 || body == "" || looksLikeHTML(body) {
		return "", false
	}
	if !strings.HasPrefix(strings.TrimSpace(body), "{") {
		return "", false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		return "", false
	}
	if raw, ok1 := m["country"]; ok1 {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil && len(strings.TrimSpace(s)) > 0 {
			return strings.TrimSpace(s), true
		}
	}
	return "", false
}
