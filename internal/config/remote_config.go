package config

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"jorbiquevpn/internal/assets"
	"jorbiquevpn/internal/constants"
	"jorbiquevpn/internal/httpsclient"
	"jorbiquevpn/internal/jsonorder"
	"jorbiquevpn/internal/paths"
	"jorbiquevpn/internal/singboxcfg"
	"jorbiquevpn/internal/utils"
)

// hy2PortRangeHyphen matches a single numeric port range written with a hyphen; sing-box expects "start:end".
var hy2PortRangeHyphen = regexp.MustCompile(`^(\d+)-(\d+)$`)

// MarshalSingBoxConfigOrdered renders a config the way it was read: keys in the
// order the profile wrote them, strings unescaped, two-space indentation.
//
// It used to impose the key order from sing-box's documentation, which meant the
// file the client ran listed the same fields in a different sequence from the
// one the server serves — unreadable in a diff, and confusing when a field was
// checked by eye.
func MarshalSingBoxConfigOrdered(cfg *jsonorder.Map) ([]byte, error) {
	return jsonorder.MarshalIndent(cfg)
}

// RemoteSingboxCachePath returns %TEMP%/JorbiqueVPN/singbox_client.json (last successful fetch).
func RemoteSingboxCachePath() string {
	dir := paths.TempAppDir()
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "singbox_client.json")
}

// RemoteNaiveCachePath is the naive profile's own cache. Apart from the main
// one, so switching modes never turns the protocol list into "naive" and back.
func RemoteNaiveCachePath() string {
	dir := paths.TempAppDir()
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "singbox_client.naive.json")
}

// profileFetchTimeout bounds the profile download. Every connection starts with
// it, so it is the delay a user waits through before anything happens; the
// profile is a few kilobytes, and a link that cannot deliver that in this long
// is not going to carry a tunnel either.
const profileFetchTimeout = 15 * time.Second

// FetchSingboxClientJSONOrCache downloads the remote profile and remembers it,
// falling back to the last copy that arrived when the server cannot be reached.
//
// There is no caching in front of this: which outbounds exist is the server's
// to change, and a connection is expected to act on what it says now, not on
// what it said the last time the client started.
//
// warnf is told when the fallback is used, and may be nil. That is the whole
// point of it: a connection built from a stale profile looks exactly like one
// built from a fresh profile, so "I changed the server and the client ignored
// me" is indistinguishable from "the client could not reach the server" unless
// it is said out loud.
func FetchSingboxClientJSONOrCache(warnf func(string)) ([]byte, error) {
	return fetchSingboxClientJSON(profileFetchTimeout, warnf)
}

// FetchNaiveClientJSONOrCache is FetchSingboxClientJSONOrCache for the
// NaiveProxy mode's profile.
func FetchNaiveClientJSONOrCache(warnf func(string)) ([]byte, error) {
	return fetchProfile(constants.SingboxClientNaiveURL, RemoteNaiveCachePath(), profileFetchTimeout, warnf)
}

// uiProfileFetchTimeout is the same download on the path that opens a window.
// Shorter than the connecting one: a settings window that takes fifteen seconds
// to draw its checkboxes reads as a hang, and the cache behind it is good
// enough to draw with.
const uiProfileFetchTimeout = 5 * time.Second

// AvailableProxyTagsFresh is the outbound list the server offers now, for the
// window that shows a checkbox per outbound.
//
// It downloads rather than reading the cache. The cache is only ever written by
// a connection or by the launch-time warm-up, so a window opened after the
// server changed its profile would otherwise show the list from whenever the
// client last connected — which is exactly when someone is looking at it to
// find out what changed. A failed download falls back to the cache, so the
// window still draws when the server is unreachable.
func AvailableProxyTagsFresh() []string {
	body, err := fetchSingboxClientJSON(uiProfileFetchTimeout, nil)
	if err != nil {
		return nil
	}
	return ProxyTagsFromJSON(body)
}

func fetchSingboxClientJSON(timeout time.Duration, warnf func(string)) ([]byte, error) {
	return fetchProfile(constants.SingboxClientURL, RemoteSingboxCachePath(), timeout, warnf)
}

func fetchProfile(remoteURL, cachePath string, timeout time.Duration, warnf func(string)) ([]byte, error) {
	u, err := url.Parse(remoteURL)
	if err != nil {
		return loadCacheOrErr(cachePath, err, warnf)
	}
	client := httpsclient.ForHost(u.Host, timeout)
	req, err := http.NewRequest(http.MethodGet, remoteURL, nil)
	if err != nil {
		return loadCacheOrErr(cachePath, err, warnf)
	}
	req.Header.Set("Accept", "application/json")
	// The profile decides what the tunnel is built from, so an intermediary's
	// copy of a previous one is not good enough.
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := client.Do(req)
	if err != nil {
		return loadCacheOrErr(cachePath, err, warnf)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return loadCacheOrErr(cachePath, err, warnf)
	}
	if resp.StatusCode != http.StatusOK || len(body) == 0 || !json.Valid(body) {
		return loadCacheOrErr(cachePath, fmt.Errorf("remote sing-box profile: HTTP %d", resp.StatusCode), warnf)
	}
	_ = os.WriteFile(cachePath, body, 0644)
	return body, nil
}

func loadRemoteCacheOrErr(cause error, warnf func(string)) ([]byte, error) {
	return loadCacheOrErr(RemoteSingboxCachePath(), cause, warnf)
}

func loadCacheOrErr(p string, cause error, warnf func(string)) ([]byte, error) {
	data, err := os.ReadFile(p)
	if err != nil || len(data) == 0 || !json.Valid(data) {
		if cause != nil {
			return nil, fmt.Errorf("%w (no valid cache at %s)", cause, p)
		}
		return nil, fmt.Errorf("no valid cache at %s", p)
	}
	if warnf != nil {
		reason := "unknown error"
		if cause != nil {
			reason = cause.Error()
		}
		warnf("Could not fetch the VPN profile (" + reason + "); connecting with the last one downloaded, " +
			"so any change made on the server since then is not applied")
	}
	return data, nil
}

// MergeRemoteWithTunShell builds the config from the remote profile, filling in
// from the embedded tun template only what the profile does not say.
//
// The profile used to be read for three keys — dns, outbounds and route — and
// everything else it carried was dropped on the floor. That made every new
// section the server wanted to ship (an api service, ntp, certificates) wait
// for a client release, and dropping half of a
// profile left the rest pointing at tags that no longer existed. The profile is
// the whole config now; the template supplies the parts only this machine knows
// — the TUN inbound, where the log goes, where
// the cache lives.
func MergeRemoteWithTunShell(remoteJSON []byte) (*jsonorder.Map, error) {
	tplData, err := assets.FS.ReadFile("tun.json")
	if err != nil {
		return nil, err
	}
	shell := jsonorder.New()
	if err := jsonorder.Unmarshal(tplData, shell); err != nil {
		return nil, err
	}
	remote := jsonorder.New()
	if err := jsonorder.Unmarshal(remoteJSON, remote); err != nil {
		return nil, err
	}
	// The profile's own order first, so the config the client runs reads like the
	// one the server serves; the template fills in behind it.
	cfg := jsonorder.New()
	for _, k := range remote.Keys() {
		v, _ := remote.Get(k)
		cfg.Set(k, v)
	}
	for _, k := range shell.Keys() {
		if cfg.Has(k) {
			continue
		}
		v, _ := shell.Get(k)
		cfg.Set(k, v)
	}
	return cfg, nil
}

func fixPathsInConfig(cfg *jsonorder.Map) {
	dataDir := paths.DataDir()
	logsDir := strings.ReplaceAll(filepath.Join(dataDir, "logs"), "\\", "/")
	cacheDir := strings.ReplaceAll(filepath.Join(dataDir, "cache"), "\\", "/")

	if log, ok := getMap(cfg, "log"); ok {
		if out := log.GetString("output"); out != "" {
			log.Set("output", utils.RelocatePath(out, "logs/", logsDir))
		}
	}
	if exp, ok := getMap(cfg, "experimental"); ok {
		if cf, ok := getMap(exp, "cache_file"); ok {
			if path := cf.GetString("path"); path != "" {
				cf.Set("path", utils.RelocatePath(path, "cache/", cacheDir))
			}
		}
	}
}

// NormalizeHysteria2ServerPorts rewrites hysteria2 server_ports entries "2080-3000" → "2080:3000".
// Remote profiles sometimes use a hyphen; sing-box expects a colon for port ranges.
func NormalizeHysteria2ServerPorts(cfg *jsonorder.Map) {
	if cfg == nil {
		return
	}
	ob, ok := getSlice(cfg, "outbounds")
	if !ok {
		return
	}
	for _, o := range ob {
		m, ok := o.(*jsonorder.Map)
		if !ok || m == nil {
			continue
		}
		if m.GetString("type") != "hysteria2" {
			continue
		}
		sp, ok := getSlice(m, "server_ports")
		if !ok || len(sp) == 0 {
			continue
		}
		normalizePortRanges(sp)
		m.Set("server_ports", sp)
	}
}

func normalizePortRanges(sp []any) {
	for i, v := range sp {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if sub := hy2PortRangeHyphen.FindStringSubmatch(strings.TrimSpace(s)); len(sub) == 3 {
			sp[i] = sub[1] + ":" + sub[2]
		}
	}
}

// OutboundEnabled returns whether tag is enabled; missing key defaults to true.
func OutboundEnabled(enabled map[string]bool, tag string) bool {
	if enabled == nil {
		return true
	}
	v, ok := enabled[tag]
	if !ok {
		return true
	}
	return v
}

// ApplyUserUUID sets credentials from the single user UUID on the outbounds
// named by tags: ProxyTagsFromConfig of the profile before filtering. Filtering
// can leave a transport helper with nothing detouring through it, and read
// afresh it would look like a proxy and take the user's credential.
// Returns an error if the ones still in cfg have no field to put it in.
func ApplyUserUUID(cfg *jsonorder.Map, uuid string, tags []string) error {
	uuid = strings.TrimSpace(uuid)
	if uuid == "" || cfg == nil {
		return nil
	}
	ob, ok := getSlice(cfg, "outbounds")
	if !ok {
		return fmt.Errorf("outbounds: missing or not an array")
	}
	vpnTag := make(map[string]struct{})
	for _, t := range tags {
		vpnTag[t] = struct{}{}
	}
	// Counted across the whole profile, not per outbound: an outbound that
	// carries its credential already is the server's business, but a profile
	// with nowhere at all to put the user's is one no tunnel can authenticate
	// through, and that is worth refusing before sing-box is started on it.
	present, filled := 0, 0
	for _, o := range ob {
		m, ok := o.(*jsonorder.Map)
		if !ok || m == nil {
			continue
		}
		tag := m.GetString("tag")
		if _, isVPN := vpnTag[tag]; !isVPN {
			continue
		}
		present++
		filled += singboxcfg.ApplyCredential(m, uuid)
	}
	// Nowhere to put it is only a problem when there was somewhere to look:
	// with every protocol switched off the config has no proxy outbound left.
	// The tags were read before filtering, so the ones the user turned off are
	// not counted.
	if filled == 0 && present > 0 {
		return fmt.Errorf("the profile leaves no empty field in any outbound to put the user's credential in")
	}
	return nil
}

func filterOutboundsAndUrltests(cfg *jsonorder.Map, enabled map[string]bool) error {
	if cfg == nil {
		return nil
	}
	ob, ok := getSlice(cfg, "outbounds")
	if !ok {
		return fmt.Errorf("outbounds missing or invalid")
	}
	// Which proxies a build keeps, and what a group may still name, is the same
	// rule on both clients, so it lives in singboxcfg.
	filtered, err := singboxcfg.FilterOutbounds(ob, cfg, enabled)
	if err != nil {
		return err
	}
	cfg.Set("outbounds", filtered)

	// The groups that have nothing left to choose between are dropped by the
	// pruner, and BuildRuntimeConfig is what checks that some tunnel survived.
	return nil
}
