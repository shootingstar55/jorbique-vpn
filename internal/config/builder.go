package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"jorbiquevpn/internal/dashboard"
	"jorbiquevpn/internal/jsonorder"
	"jorbiquevpn/internal/paths"
	"jorbiquevpn/internal/singboxcfg"
)

// RuntimeOptions contains the user settings applied to a downloaded profile.
type RuntimeOptions struct {
	UserUUID        string
	OutboundEnabled map[string]bool
	// NaiveMode builds from the profile's NaiveProxy outbounds alone. Off, they
	// are left out whatever OutboundEnabled says (singboxcfg.ModeEnabled).
	NaiveMode                                bool
	IgnoredProcessNames, IgnoredProcessPaths []string
	SelectedNode                             string
	UserDirectDomains                        []string
}

// ErrNoNaiveOutbound is the NaiveProxy mode against a profile that offers no
// naive outbound, which would otherwise read as every protocol switched off.
var ErrNoNaiveOutbound = errors.New("the server's profile has no NaiveProxy outbound")

// BuildRuntimeConfig generates runtime_config.json from the remote sing-box profile merged into the embedded tun template,
// then applies user UUID, outbound toggles, ignored processes, direct domains, and the node the user picked in the selector.
func BuildRuntimeConfig(remoteJSON []byte, options RuntimeOptions) (string, error) {
	cfg, err := MergeRemoteWithTunShell(remoteJSON)
	if err != nil {
		return "", err
	}

	fixPathsInConfig(cfg)
	if err := dashboard.ConfigureAPI(cfg, ""); err != nil {
		return "", err
	}
	NormalizeHysteria2ServerPorts(cfg)

	// Read before filtering: a transport kept only because a group nothing
	// routes to still names it has, once its proxies are off, nothing detouring
	// through it, and read afresh it looks like a proxy of its own.
	proxyTags := ProxyTagsFromConfig(cfg)
	enabled, err := modeOutboundEnabled(cfg, proxyTags, options)
	if err != nil {
		return "", err
	}
	if err := filterOutboundsAndUrltests(cfg, enabled); err != nil {
		return "", err
	}

	if err := ApplyUserUUID(cfg, options.UserUUID, proxyTags); err != nil {
		return "", err
	}

	// The tunnel is the profile's proxy outbounds and nothing else: the client
	// runs no endpoints (OpenVPN, WireGuard, Tailscale), so whatever resolves
	// through one or routes to one goes with it.
	cfg.Delete("endpoints")
	singboxcfg.PruneDanglingReferences(cfg)

	// Every protocol switched off leaves a config that starts and sends every
	// packet straight out, which no one asked "connect" for.
	if !singboxcfg.HasTunnel(cfg) {
		return "", fmt.Errorf("enable at least one protocol")
	}

	// After pruning, so a node the user picked and then switched off cannot be
	// written into a selector that no longer offers it.
	singboxcfg.SetSelectorDefault(cfg, options.SelectedNode)

	applyIgnoredProcesses(cfg, options.IgnoredProcessNames)
	applyIgnoredProcessPaths(cfg, options.IgnoredProcessPaths)
	applyUserDirectDomainSuffixes(cfg, options.UserDirectDomains)

	singboxcfg.SortRoot(cfg)

	contentBytes, err := jsonorder.MarshalIndent(cfg)
	if err != nil {
		return "", err
	}

	outPath := paths.RuntimeConfigPath()
	return outPath, os.WriteFile(outPath, contentBytes, 0644)
}

// modeOutboundEnabled is the user's toggles as the chosen mode reads them.
func modeOutboundEnabled(cfg *jsonorder.Map, proxyTags []string, options RuntimeOptions) (map[string]bool, error) {
	obsRaw, _ := cfg.Get("outbounds")
	obs, _ := obsRaw.([]any)
	if options.NaiveMode && len(singboxcfg.NaiveTags(obs)) == 0 {
		return nil, ErrNoNaiveOutbound
	}
	return singboxcfg.ModeEnabled(obs, proxyTags, options.OutboundEnabled, options.NaiveMode), nil
}

func applyIgnoredProcesses(cfg *jsonorder.Map, ignored []string) {
	names := []any{}
	seen := map[string]bool{}
	add := func(name string) {
		key := strings.ToLower(name)
		if key != "" && !seen[key] {
			seen[key] = true
			names = append(names, name)
		}
	}
	for _, name := range ignored {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		add(name)
		if strings.HasSuffix(strings.ToLower(name), ".exe") {
			add(strings.TrimSpace(name[:len(name)-4]))
		} else {
			add(name + ".exe")
		}
	}
	applyDirectProcessRule(cfg, "process_name", names)
}

func bypassRuleInsertIndex(rules []any) int {
	afterDNS := 0
	for i, r := range rules {
		m, ok := r.(*jsonorder.Map)
		if !ok || m == nil {
			continue
		}
		if m.GetString("outbound") == "direct" && m.Has("source_ip_cidr") {
			return i + 1
		}
		if m.GetString("protocol") == "dns" {
			afterDNS = i + 1
		}
	}
	return afterDNS
}

// applyIgnoredProcessPaths adds sing-box route rules for absolute process paths.
func applyIgnoredProcessPaths(cfg *jsonorder.Map, paths []string) {
	seen := map[string]bool{}
	var values []any
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		path = filepath.Clean(path)
		key := strings.ToLower(path)
		if !filepath.IsAbs(path) || seen[key] {
			continue
		}
		seen[key] = true
		values = append(values, path)
	}
	applyDirectProcessRule(cfg, "process_path", values)
}

func applyDirectProcessRule(cfg *jsonorder.Map, field string, values []any) {
	if len(values) == 0 || cfg == nil {
		return
	}
	route, ok := getMap(cfg, "route")
	if !ok || route == nil {
		return
	}
	rules, ok := getSlice(route, "rules")
	if !ok || len(rules) == 0 {
		return
	}
	if mergeDirectProcessRule(rules, field, values) {
		return
	}
	rule := jsonorder.New()
	rule.Set(field, values)
	rule.Set("action", "route")
	rule.Set("outbound", "direct")
	insertAt := bypassRuleInsertIndex(rules)
	route.Set("rules", append(rules[:insertAt], append([]any{rule}, rules[insertAt:]...)...))
}

func mergeDirectProcessRule(rules []any, field string, values []any) bool {
	for _, r := range rules {
		rule, ok := r.(*jsonorder.Map)
		if !ok || rule == nil || rule.GetString("outbound") != "direct" {
			continue
		}
		existing, ok := getSlice(rule, field)
		if !ok {
			continue
		}
		rule.Set(field, mergeProcessValues(existing, values))
		return true
	}
	return false
}

func mergeProcessValues(existing, values []any) []any {
	seen := map[string]bool{}
	for _, value := range existing {
		if name, ok := value.(string); ok {
			seen[strings.ToLower(name)] = true
		}
	}
	for _, value := range values {
		if name, ok := value.(string); ok && !seen[strings.ToLower(name)] {
			existing = append(existing, name)
		}
	}
	return existing
}

// ruZoneDirectRuleLegacyPattern was the old single-regex form in embedded templates (still recognized for insert position).
const ruZoneDirectRuleLegacyPattern = `.*\.(ru|su|xn--p1ai)$`

func isRuZoneDirectRule(m *jsonorder.Map) bool {
	if m == nil || m.GetString("outbound") != "direct" {
		return false
	}
	arr, ok := getSlice(m, "domain_regex")
	if !ok {
		return false
	}
	for _, v := range arr {
		s, ok := v.(string)
		if !ok {
			continue
		}
		switch s {
		case ruZoneDirectRuleLegacyPattern, `.*\.ru$`, `.*\.su$`, `.*\.xn--p1ai$`, `.*\.xn--p1ag$`:
			return true
		}
	}
	return false
}

func applyUserDirectDomainSuffixes(cfg *jsonorder.Map, domains []string) {
	if len(domains) == 0 || cfg == nil {
		return
	}
	var asIface []interface{}
	var asRegex []string
	for _, p := range domains {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		asIface = append(asIface, p)
		asRegex = append(asRegex, `(.*\.)?`+regexp.QuoteMeta(p)+`$`)
	}
	if len(asIface) == 0 {
		return
	}
	route, ok := getMap(cfg, "route")
	if !ok || route == nil {
		return
	}
	rules, ok := getSlice(route, "rules")
	if !ok {
		return
	}
	insertAt, regexRule := directDomainInsertPoint(rules)
	domainRule := jsonorder.New()
	domainRule.Set("domain", asIface)
	domainRule.Set("action", "route")
	domainRule.Set("outbound", "direct")
	rules = append(rules[:insertAt], append([]any{domainRule}, rules[insertAt:]...)...)
	if regexRule != nil {
		existing, _ := regexRule.Get("domain_regex")
		regexRule.Set("domain_regex", mergeRegexInterfaces(existing, asRegex))
	} else {
		regexRule = jsonorder.New()
		regexRule.Set("domain_regex", stringSliceToInterface(asRegex))
		regexRule.Set("action", "route")
		regexRule.Set("outbound", "direct")
		rules = append(rules, regexRule)
	}
	route.Set("rules", rules)
	applyDirectDomainsToDNS(cfg, asIface)
}

func directDomainInsertPoint(rules []any) (int, *jsonorder.Map) {
	afterPrivate := -1
	for i, r := range rules {
		rule, ok := r.(*jsonorder.Map)
		if !ok || rule == nil {
			continue
		}
		if isRuZoneDirectRule(rule) {
			return i + 1, rule
		}
		if private, _ := rule.Get("ip_is_private"); private == true && afterPrivate < 0 {
			afterPrivate = i + 1
		}
	}
	if afterPrivate >= 0 {
		return afterPrivate, nil
	}
	return len(rules), nil
}

// applyDirectDomainsToDNS sends the same names to the local resolver.
//
// Routing them direct is only half of it: the lookup still went to the profile's
// DoH server, through the tunnel, so a name meant to bypass the VPN was resolved
// by the other side of it — and a host that only answers on the local network
// did not resolve at all. The profile names its own local server; a profile
// without one has nothing to point at, so nothing is added.
func applyDirectDomainsToDNS(cfg *jsonorder.Map, domains []any) {
	if len(domains) == 0 {
		return
	}
	dns, ok := getMap(cfg, "dns")
	if !ok || dns == nil {
		return
	}
	local := localDNSServerTag(dns)
	if local == "" {
		return
	}
	suffixes := make([]any, 0, len(domains))
	for _, d := range domains {
		if name, ok := d.(string); ok {
			suffixes = append(suffixes, "."+strings.TrimPrefix(name, "."))
		}
	}
	rule := jsonorder.New()
	rule.Set("domain", domains)
	rule.Set("domain_suffix", suffixes)
	rule.Set("action", "route")
	rule.Set("server", local)

	// First: the first matching rule wins, and these names are the user's own
	// answer to where they should resolve.
	rules, _ := getSlice(dns, "rules")
	dns.Set("rules", append([]any{rule}, rules...))
}

// localDNSServerTag is the profile's own local resolver — the one that asks the
// system, rather than a server reached through the tunnel.
func localDNSServerTag(dns *jsonorder.Map) string {
	servers, _ := getSlice(dns, "servers")
	for _, s := range servers {
		m, ok := s.(*jsonorder.Map)
		if !ok || m == nil {
			continue
		}
		if strings.EqualFold(m.GetString("type"), "local") {
			return strings.TrimSpace(m.GetString("tag"))
		}
	}
	return ""
}

func mergeRegexInterfaces(existing any, extra []string) []any {
	seen := map[string]bool{}
	out := []any{}
	if arr, ok := existing.([]any); ok {
		for _, v := range arr {
			s, ok := v.(string)
			if !ok || seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, s := range extra {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func stringSliceToInterface(in []string) []any {
	out := make([]interface{}, 0, len(in))
	for _, s := range in {
		out = append(out, s)
	}
	return out
}
