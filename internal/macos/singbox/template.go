package singbox

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"jorbiquevpn/internal/constants"
	"jorbiquevpn/internal/dashboard"
	"jorbiquevpn/internal/httpsclient"
	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/macos/fsatomic"
	"jorbiquevpn/internal/singboxcfg"
	"jorbiquevpn/internal/utils"
)

// ProxyTagsFromOutbounds lists the tags a user can choose between, in the order
// the server means them to be tried. entries is singboxcfg.RouteEntries of the
// profile the outbounds came from.
func ProxyTagsFromOutbounds(outbounds []any, entries []string) []string {
	return singboxcfg.ProxyTags(outbounds, entries)
}

// defaultLocalRoot is what a config needs from this machine rather than from the
// profile: the TUN inbound and a log section.
func defaultLocalRoot() *OrderedMap {
	root := NewOrderedMap()
	log := NewOrderedMap()
	log.Set("level", "warn")
	log.Set("timestamp", true)
	log.Set("output", "logs/singbox.log")
	root.Set("log", log)
	root.Set("inbounds", defaultTunInbounds())
	return root
}

func defaultTunInbounds() []any {
	tun := NewOrderedMap()
	tun.Set("type", "tun")
	tun.Set("tag", "tun-in")
	tun.Set("address", []any{"172.31.255.1/30"})
	tun.Set("auto_route", true)
	tun.Set("strict_route", true)
	tun.Set("mtu", 1280)
	tun.Set("udp_timeout", "5m")
	return []any{tun}
}

// normalizeEnabledProxyTags picks what to build the config from: the tags the
// user chose, kept in the server's order and filtered to what the server still
// offers.
//
// Two cases answer with everything available. A configuration that names no
// tags has no opinion — a fresh install, or one that has never opened the
// panel — and a configuration whose every tag has vanished is describing a
// server that has moved on. Refusing to start there would mean a rename on the
// server bricks every client until each user reopens a menu.
func normalizeEnabledProxyTags(tags, available []string) []string {
	if len(available) == 0 {
		return nil
	}
	chosen := map[string]struct{}{}
	for _, t := range tags {
		if t = strings.TrimSpace(t); t != "" {
			chosen[strings.ToLower(t)] = struct{}{}
		}
	}
	if len(chosen) == 0 {
		return append([]string(nil), available...)
	}

	var out []string
	for _, t := range available {
		if _, ok := chosen[strings.ToLower(t)]; ok {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return append([]string(nil), available...)
	}
	return out
}

// shippedDefaultOutbounds is the positive list the packaged vpn_cfg.json
// carried until it was emptied: the five outbounds that existed when it was
// written. It is not a list of known protocols and nothing else may be added to
// it — it exists only to recognise a specific historical file.
var shippedDefaultOutbounds = []string{"vless-reality", "vless-grpc", "tuic", "hysteria2", "anytls"}

// IsUnchosenShippedDefault reports whether a stored choice is the packaged
// default that nobody ever changed, rather than something a user picked.
//
// The .pkg used to install an enabled_outbounds naming those five tags, so
// every machine installed before that was fixed carries a positive list it was
// given, not one it chose — and a positive list is read as a deliberate choice,
// which is what keeps an outbound the server adds later permanently off.
// preinstall preserves vpn_cfg.json across upgrades, so those machines never
// stop carrying it.
//
// Two things have to hold together, and neither is enough alone:
//
//   - the list is exactly those five tags, and
//   - there is no disabled_outbounds key at all (disabled is nil, not empty).
//
// The second is what makes this safe. Every write through the panel goes
// through routerules.PatchVpnCfgSingboxUUIDAndOutbounds, which always adds a
// disabled_outbounds key — so its absence means the singbox section has never
// been written by a client that records refusals, and the five tags in it are
// the ones the package put there.
//
// The remaining case is a genuinely old configuration where the user did pick
// those five by hand. Reading it as "no opinion" gives them the same five back,
// because that was everything on offer at the time: naming all of them refuses
// nothing. The only thing that changes is that a tag added since is on, which
// is the answer they never got asked about.
//
// Nothing is rewritten on disk. This is how the file is read, so a user who
// opens the panel once writes a disabled_outbounds key and stops matching.
func IsUnchosenShippedDefault(enabled, disabled []string) bool {
	// A recorded "no" — even an empty one — means a client that understands
	// refusals has written this section. Whatever is in the positive list was
	// chosen, not shipped.
	if disabled != nil {
		return false
	}
	if len(enabled) != len(shippedDefaultOutbounds) {
		return false
	}
	have := map[string]struct{}{}
	for _, t := range enabled {
		if t = strings.TrimSpace(t); t != "" {
			have[strings.ToLower(t)] = struct{}{}
		}
	}
	if len(have) != len(shippedDefaultOutbounds) {
		return false
	}
	for _, t := range shippedDefaultOutbounds {
		if _, ok := have[t]; !ok {
			return false
		}
	}
	return true
}

// EnabledProxyTags decides which outbounds a config is built from.
//
// The user's "no" is what is remembered: everything the profile offers is on
// unless they turned it off. That is the only rule under which the server can
// add a protocol and have it work — a stored list of "yes" cannot tell an
// outbound that was refused from one that did not exist when the choice was
// made.
//
// enabled is the older positive list, still read from configurations written
// before disabled existed. Explicit refusals are never reset, even all of them:
// the builder then refuses, since there is no tunnel left.
func EnabledProxyTags(available, enabled, disabled []string) []string {
	if len(available) == 0 {
		return nil
	}
	if disabled == nil && len(enabled) > 0 && !IsUnchosenShippedDefault(enabled, disabled) {
		return normalizeEnabledProxyTags(enabled, available)
	}

	off := map[string]struct{}{}
	for _, t := range disabled {
		if t = strings.TrimSpace(t); t != "" {
			off[strings.ToLower(t)] = struct{}{}
		}
	}
	var out []string
	for _, t := range available {
		if _, no := off[strings.ToLower(t)]; !no {
			out = append(out, t)
		}
	}
	// Every protocol refused is a real answer, not a mistake to correct; the
	// builder says so rather than turning them all back on.
	return out
}

func downloadURL(rawURL string, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cache-Control", "no-cache")
	host := ""
	if u, uerr := url.Parse(rawURL); uerr == nil {
		host = u.Host
	}
	cl := httpsclient.ForHost(host, timeout)
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, core.BadConfig("template download: HTTP " + resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, core.BadConfig("template download: empty body")
	}
	return body, nil
}

// remoteTemplateCachePath is where the last profile that parsed is kept — next
// to the config it feeds, so it survives everything but an uninstall.
func remoteTemplateCachePath(baseConfigPath string) string {
	return filepath.Join(filepath.Dir(baseConfigPath), "singbox_client.json")
}

// fetchTemplateOrCache downloads the profile and remembers it, falling back to
// the last good copy when the server cannot be reached. Without this a blip on
// the update host meant the VPN would not start at all, even though the profile
// that started it an hour ago was still perfectly good.
func fetchTemplateOrCache(cachePath string, timeout time.Duration, warnf func(string)) ([]byte, error) {
	body, err := downloadURL(constants.SingboxClientURL, timeout)
	if err == nil && json.Valid(body) {
		// In one step: the menu bar and set-singbox-config read this file from
		// other processes, and a half-written one reads as no profile at all.
		_ = fsatomic.WriteFile(cachePath, body, 0o644)
		return body, nil
	}
	reason := "the downloaded profile is not valid JSON"
	if err != nil {
		reason = err.Error()
	}
	cached, cerr := os.ReadFile(cachePath)
	if cerr != nil || len(cached) == 0 || !json.Valid(cached) {
		return nil, core.BadConfig("template download: " + reason + " (no usable cache at " + cachePath + ")")
	}
	if warnf != nil {
		warnf("sing-box profile: " + reason + " — starting from the cached copy at " + cachePath)
	}
	return cached, nil
}

// RefreshRemoteTemplate downloads the profile into the cache beside the config,
// without building anything from it.
//
// The panels read that cache: which protocols exist and what the selector can
// point at. It used to be filled only by a connect, so a fresh install — or a
// machine whose config directory was deleted — opened the VPN Config window on
// "the profile has not been downloaded yet" and had nothing to offer. This is
// what lets the menu bar load the profile at app start.
func RefreshRemoteTemplate(baseConfigPath string, timeout time.Duration, warnf func(string)) error {
	cachePath := remoteTemplateCachePath(baseConfigPath)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return err
	}
	_, err := fetchTemplateOrCache(cachePath, timeout, warnf)
	return err
}

// localRootOrDefault reads the local base config as strict JSON, or returns the
// built-in local half when the file is missing or does not parse.
func localRootOrDefault(path string) *OrderedMap {
	data, err := os.ReadFile(path)
	if err != nil {
		return defaultLocalRoot()
	}
	parsed := NewOrderedMap()
	if err := unmarshalToOrderedMap(data, parsed); err != nil {
		return defaultLocalRoot()
	}
	return parsed
}

// BuildConfigFromRemoteTemplate downloads the remote sing-box client template,
// replaces uuid/password, prunes proxy outbounds based on enabled tags, and merges with base local inbounds.
// warnf, when set, reports a fallback to the cached profile; it may be nil.
func BuildConfigFromRemoteTemplate(settings core.SingboxSection, baseConfigPath string, timeout time.Duration, warnf func(string)) (*OrderedMap, error) {
	// The local half — the TUN inbound and where the log goes — is built here
	// when there is no file to read it from, so a machine whose config was
	// deleted (or that has never run) gets one from the downloaded profile
	// alone.
	baseRoot := localRootOrDefault(baseConfigPath)
	localAPISecret := dashboard.APISecret(baseRoot)

	body, err := fetchTemplateOrCache(remoteTemplateCachePath(baseConfigPath), timeout, warnf)
	if err != nil {
		return nil, err
	}
	remoteRoot := NewOrderedMap()
	if err := unmarshalToOrderedMap(body, remoteRoot); err != nil {
		return nil, core.BadConfig("template parse: " + err.Error())
	}

	newOutbounds, err := templateOutbounds(remoteRoot, settings, warnf)
	if err != nil {
		return nil, err
	}

	// The profile is the config; the local base only fills in what it does not
	// say. Reading three keys out of it — dns, outbounds, route — meant every
	// section the server added (an api service, ntp) was dropped on the floor.
	// What stays local is what only this machine knows: the TUN inbound and
	// where the log goes.
	root := NewOrderedMap()
	for _, k := range remoteRoot.Keys() {
		v, _ := remoteRoot.Get(k)
		root.Set(k, v)
	}
	for _, k := range baseRoot.Keys() {
		if root.Has(k) {
			continue
		}
		v, _ := baseRoot.Get(k)
		root.Set(k, v)
	}
	root.Set("outbounds", newOutbounds)

	// The tunnel is the profile's proxy outbounds and nothing else: the client
	// runs no endpoints (OpenVPN, WireGuard, Tailscale), so whatever resolves
	// through one or routes to one goes with it.
	root.Delete("endpoints")
	singboxcfg.PruneDanglingReferences(root)

	if !singboxcfg.HasTunnel(root) {
		return nil, core.BadConfig("enable at least one protocol")
	}

	// The node the user picked becomes the selector's default, so a restart comes
	// back on it. Switching a running tunnel goes through the API instead, and
	// that choice dies with the process — this is the half that survives it.
	// After the pruning, so a node that has since been switched off cannot be
	// written into a group that no longer offers it.
	singboxcfg.SetSelectorDefault(root, settings.SelectedNode)

	if err := dashboard.ConfigureAPI(root, localAPISecret); err != nil {
		return nil, core.BadConfig(err.Error())
	}
	return root, nil
}

func templateOutbounds(remoteRoot *OrderedMap, settings core.SingboxSection, warnf func(string)) ([]any, error) {
	u := strings.TrimSpace(settings.UUID)
	outboundsRaw, ok := remoteRoot.Get("outbounds")
	if !ok {
		return nil, core.BadConfig("template missing outbounds")
	}
	outbounds, ok := outboundsRaw.([]any)
	if !ok {
		return nil, core.BadConfig("template outbounds is not array")
	}
	// What the server offers decides what can be enabled. A tag the
	// configuration still names but the template no longer has is dropped here;
	// one the server added is picked up without a client release.
	entries := singboxcfg.RouteEntries(remoteRoot)
	available := ProxyTagsFromOutbounds(outbounds, entries)
	if len(available) == 0 {
		return nil, core.BadConfig("the sing-box profile has no proxy outbounds")
	}
	if settings.NaiveMode && len(singboxcfg.NaiveTags(outbounds)) == 0 {
		return nil, core.BadConfig("the sing-box profile has no NaiveProxy outbound")
	}
	// The toggles answer for the protocols. NaiveProxy is a mode of its own,
	// which ModeEnabled below applies whatever the toggles say.
	protocols := singboxcfg.ProtocolTags(outbounds, entries)
	enabled := EnabledProxyTags(protocols, settings.EnabledOutbounds, settings.DisabledOutbounds)
	if warnf != nil && !settings.NaiveMode {
		if dropped := missingFrom(settings.EnabledOutbounds, protocols); len(dropped) > 0 {
			warnf("sing-box profile: the server no longer offers " + strings.Join(dropped, ", "))
		}
	}

	// Which proxies a build keeps, and what a group may still name, lives in
	// singboxcfg. The groups are plumbing it trims rather than drops: a selector
	// the resolver detours through has to survive, or pruning takes the
	// profile's DNS with it.
	enabledSet := make(map[string]bool, len(available))
	for _, tag := range available {
		enabledSet[tag] = false
	}
	for _, tag := range enabled {
		enabledSet[tag] = true
	}
	enabledSet = singboxcfg.ModeEnabled(outbounds, available, enabledSet, settings.NaiveMode)
	// The UUID is the credential the protocols authenticate with.
	if u == "" && (settings.NaiveMode || len(enabled) > 0) {
		return nil, core.BadConfig("sing-box uuid is empty")
	}
	newOutbounds, err := singboxcfg.FilterOutbounds(outbounds, remoteRoot, enabledSet)
	if err != nil {
		return nil, core.BadConfig(err.Error())
	}

	// Replace auth values by the rule in singboxcfg.ApplyCredential. Only the
	// proxies the user can pick: a transport they detour through carries the
	// server's credential, not the user's.
	applyProxyCredentials(newOutbounds, enabledSet, u)
	return newOutbounds, nil
}

func applyProxyCredentials(outbounds []any, enabled map[string]bool, credential string) {
	if credential == "" {
		return
	}
	for _, ob := range outbounds {
		om, ok := ob.(*OrderedMap)
		if ok && enabled[om.GetString("tag")] {
			singboxcfg.ApplyCredential(om, credential)
		}
	}
}

func unmarshalToOrderedMap(data []byte, m *OrderedMap) error {
	return m.UnmarshalJSON(data)
}

// missingFrom lists the tags a configuration names that the server no longer
// has, so the client can say so once rather than leaving the user wondering
// why a protocol disappeared from the menu.
func missingFrom(want, available []string) []string {
	if len(want) == 0 {
		return nil
	}
	have := map[string]struct{}{}
	for _, t := range available {
		have[strings.ToLower(strings.TrimSpace(t))] = struct{}{}
	}
	var gone []string
	for _, t := range want {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := have[strings.ToLower(t)]; !ok {
			gone = append(gone, t)
		}
	}
	return gone
}

// AvailableProxyTags lists the protocols of the profile as it was last
// downloaded, for callers that need the list without building a config — the
// menu that shows a checkbox per protocol. NaiveProxy is a mode, not a
// checkbox, so it is not among them.
//
// Reads the cache only. The menu bar opens this panel on a click and must not
// wait on the network; the cache is refreshed by RefreshRemoteTemplate and on
// every successful build, and survives everything short of an uninstall.
func AvailableProxyTags(baseConfigPath string) []string {
	data, err := os.ReadFile(remoteTemplateCachePath(baseConfigPath))
	if err != nil || len(data) == 0 {
		return nil
	}
	root := NewOrderedMap()
	if err := unmarshalToOrderedMap(data, root); err != nil {
		return nil
	}
	raw, ok := root.Get("outbounds")
	if !ok {
		return nil
	}
	outbounds, ok := raw.([]any)
	if !ok {
		return nil
	}
	return singboxcfg.ProtocolTags(outbounds, singboxcfg.RouteEntries(root))
}

// NormalizeLogOutput points the profile's log file at this machine's log
// directory. A profile that names one names it on the machine that wrote it,
// and a relative name resolves against sing-box's working directory, which is
// not the same for a daemon start and a run from a terminal.
//
// A profile that says nothing about the log is left alone: sing-box then writes
// to stdout, which is where launchd already collects it.
func NormalizeLogOutput(root *OrderedMap, logDir string) {
	if root == nil || strings.TrimSpace(logDir) == "" {
		return
	}
	raw, ok := root.Get("log")
	if !ok {
		return
	}
	log, ok := raw.(*OrderedMap)
	if !ok {
		return
	}
	out, ok := log.Get("output")
	if !ok {
		return
	}
	s, ok := out.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return
	}
	log.Set("output", utils.RelocatePath(s, "logs/", logDir))
}
