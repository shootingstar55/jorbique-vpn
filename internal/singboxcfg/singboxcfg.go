// Package singboxcfg holds the sing-box config pipeline: which outbounds a
// user kept, and the pruning that keeps a config loadable once something is
// taken out of it.
package singboxcfg

import (
	"slices"
	"strings"

	"jorbiquevpn/internal/jsonorder"
)

// pruneDanglingReferences removes everything left naming something the config no
// longer has.
//
// Taking a tunnel out is never one deletion: the profile resolves through it,
// routes to it, and may dial a resolver through it in turn. sing-box refuses to
// load a config that names a tag which is not there, so a half-pruned config
// does not fail at the outbound the user turned off — it fails to start at all,
// blaming whatever dangling name it happened to read first.
//
// Rather than chase each relationship by hand, this works the way the reference
// graph does: whatever is gone makes its dependents gone too, repeatedly, until
// nothing new drops out. A resolver that dialled through a removed outbound goes
// with it; the rules that named that resolver go with the resolver; a "final"
// pointing at any of them stops pointing.
func PruneDanglingReferences(cfg *jsonorder.Map) {
	if cfg == nil {
		return
	}

	// Round after round: removing a dns server can orphan the rules that name
	// it, and (in a profile that chains them) another server that detoured
	// through it. Two passes would cover today's profile; looping covers the
	// one that adds a link.
	// A group emptied of its members goes too: sing-box rejects a group with
	// no tags, and the resolver that dialled through the group has to go with
	// it. So group pruning belongs in the same round as the dns pruning.
	// Which groups this dropped, not just that something went: a resolver that
	// dialled through a group can dial for itself once the group is gone, and
	// one that dialled through a tunnel cannot.
	droppedGroups := map[string]bool{}
	for {
		changed := pruneOutboundGroups(cfg, dialableTags(cfg), droppedGroups)
		if pruneDNSServers(cfg, dialableTags(cfg), droppedGroups) {
			changed = true
		}
		if !changed {
			break
		}
	}

	dialable := dialableTags(cfg)
	servers := dnsServerTags(cfg)

	pruneDNSRules(cfg, servers)
	pruneRouteReferences(cfg, dialable)
	pruneDomainResolvers(cfg, servers)
}

func pruneDNSRules(cfg *jsonorder.Map, servers map[string]bool) {
	if dns, ok := getMap(cfg, "dns"); ok {
		if rules, ok := getSlice(dns, "rules"); ok {
			dns.Set("rules", pruneRules(rules, "server", servers))
		}
		if final := dns.GetString("final"); final != "" && !servers[final] {
			dns.Delete("final")
		}
	}
}

func pruneRouteReferences(cfg *jsonorder.Map, dialable map[string]bool) {
	route, ok := getMap(cfg, "route")
	if !ok {
		return
	}
	if rules, ok := getSlice(route, "rules"); ok {
		route.Set("rules", pruneRules(rules, "outbound", dialable))
	}
	if final := route.GetString("final"); final != "" && !dialable[final] {
		// Without a final sing-box falls back to the first outbound, which
		// is only the same thing by luck. Name it: with the proxies gone
		// the traffic that is not routed into a tunnel goes out directly.
		if direct := directOutboundTag(cfg); direct != "" {
			route.Set("final", direct)
		} else {
			route.Delete("final")
		}
	}
	// A rule set downloads through its download_detour, and sing-box
	// refuses the config when that names nothing. Without one it
	// downloads through the default outbound, which the final above
	// still names: the fallback a resolver that dialled through a
	// dropped group gets too.
	pruneRuleSetDetours(route, dialable)
}

func pruneRuleSetDetours(route *jsonorder.Map, dialable map[string]bool) {
	sets, _ := getSlice(route, "rule_set")
	for _, s := range sets {
		if m, ok := s.(*jsonorder.Map); ok && m != nil {
			if detour := m.GetString("download_detour"); detour != "" && !dialable[detour] {
				m.Delete("download_detour")
			}
		}
	}
}

// pruneDomainResolvers drops the "domain_resolver" a section names when that
// resolver is gone. It is a dns tag like the ones in the rules, and sing-box
// refuses the config over it just the same — with a message about the outbound
// that named it rather than about the resolver that went.
func pruneDomainResolvers(cfg *jsonorder.Map, servers map[string]bool) {
	// Deleting the name is not enough when something still has a domain to
	// resolve: sing-box then refuses with "missing domain resolver for domain
	// server address". So the dangling name is repointed at a resolver that is
	// still there, and only dropped when the config has none left.
	fallback := localResolverTag(cfg)

	if route, ok := getMap(cfg, "route"); ok {
		clearDomainResolver(route, "default_domain_resolver", servers, fallback)
	}
	list, _ := getSlice(cfg, "outbounds")
	for _, item := range list {
		if m, ok := item.(*jsonorder.Map); ok {
			clearDomainResolver(m, "domain_resolver", servers, fallback)
		}
	}
}

func clearDomainResolver(m *jsonorder.Map, key string, servers map[string]bool, fallback string) {
	if m == nil {
		return
	}
	if v := m.GetString(key); v != "" && !servers[v] {
		if fallback != "" {
			m.Set(key, fallback)
		} else {
			m.Delete(key)
		}
	}
}

// dialableTags is everything a rule may route to. The client builds configs
// without endpoints, so that is the outbounds alone.
func dialableTags(cfg *jsonorder.Map) map[string]bool {
	out := map[string]bool{}
	list, _ := getSlice(cfg, "outbounds")
	for _, o := range list {
		if m, ok := o.(*jsonorder.Map); ok {
			if tag := m.GetString("tag"); tag != "" {
				out[tag] = true
			}
		}
	}
	return out
}

func dnsServerTags(cfg *jsonorder.Map) map[string]bool {
	out := map[string]bool{}
	dns, ok := getMap(cfg, "dns")
	if !ok {
		return out
	}
	servers, _ := getSlice(dns, "servers")
	for _, s := range servers {
		if m, ok := s.(*jsonorder.Map); ok {
			if tag := m.GetString("tag"); tag != "" {
				out[tag] = true
			}
		}
	}
	return out
}

// pruneDNSServers drops the resolvers that reach through something that is gone
// — an endpoint they resolve on (the client drops every endpoint), or an
// outbound they detour through. Reports whether it removed any, so the caller
// can look again.
func pruneDNSServers(cfg *jsonorder.Map, dialable map[string]bool, droppedGroups map[string]bool) bool {
	dns, ok := getMap(cfg, "dns")
	if !ok {
		return false
	}
	servers, ok := getSlice(dns, "servers")
	if !ok {
		return false
	}
	kept := []any{}
	removed := false
	for _, s := range servers {
		m, ok := s.(*jsonorder.Map)
		if !ok || m == nil {
			kept = append(kept, s)
			continue
		}
		keep, changed := pruneServerDependencies(m, dialable, droppedGroups)
		removed = removed || changed
		if !keep {
			continue
		}

		kept = append(kept, s)
	}
	if removed {
		dns.Set("servers", kept)
	}
	return removed
}

// A resolver bound to an endpoint goes with it. A public resolver whose
// proxy group was emptied can still dial directly.
func pruneServerDependencies(m *jsonorder.Map, dialable, droppedGroups map[string]bool) (keep, changed bool) {
	if name := m.GetString("endpoint"); name != "" && !dialable[name] {
		return false, true
	}
	name := m.GetString("detour")
	if name == "" || dialable[name] {
		return true, false
	}
	if !droppedGroups[name] {
		return false, true
	}
	m.Delete("detour")
	return true, true
}

// pruneRules drops the rules naming something that is gone, following logical
// rules into the rules they hold. A rule that names nothing of the kind — a
// sniff, a hijack-dns, a reject — is not about routing and is left alone.
func pruneRules(rules []any, key string, live map[string]bool) []any {
	kept := []any{}
	for _, r := range rules {
		m, ok := r.(*jsonorder.Map)
		if !ok || m == nil {
			kept = append(kept, r)
			continue
		}
		if name := m.GetString(key); name != "" && !live[name] {
			continue
		}
		if nested, ok := getSlice(m, "rules"); ok {
			inner := pruneRules(nested, key, live)
			if len(inner) == 0 {
				// A logical rule with nothing left to match on matches
				// everything, which is not what it was written to do.
				continue
			}
			m.Set("rules", inner)
		}
		kept = append(kept, r)
	}
	return kept
}

// pruneOutboundGroups trims the tag lists a group outbound holds — a urltest's
// candidates, a selector's choices — of names that are gone.
func pruneOutboundGroups(cfg *jsonorder.Map, live map[string]bool, dropped map[string]bool) bool {
	obs, ok := getSlice(cfg, "outbounds")
	if !ok {
		return false
	}
	changed := false
	keptGroups := []any{}
	for _, o := range obs {
		m, ok := o.(*jsonorder.Map)
		if !ok || m == nil {
			keptGroups = append(keptGroups, o)
			continue
		}
		keep, groupChanged := pruneOutboundGroup(m, live, dropped)
		changed = changed || groupChanged
		if keep {
			keptGroups = append(keptGroups, o)
		}
	}
	if changed {
		cfg.Set("outbounds", keptGroups)
	}
	return changed
}

func pruneOutboundGroup(m *jsonorder.Map, live, dropped map[string]bool) (bool, bool) {
	changed := false
	if typ := m.GetString("type"); typ != "urltest" && typ != "selector" {
		return true, false
	}
	list, ok := getSlice(m, "outbounds")
	if !ok {
		return true, false
	}
	kept := []any{}
	for _, v := range list {
		if s, ok := v.(string); ok && !live[s] {
			continue
		}
		kept = append(kept, v)
	}
	if len(kept) != len(list) {
		m.Set("outbounds", kept)
		changed = true
	}
	// "missing tags" is what sing-box calls a group with nothing in it, and
	// it refuses the whole config over one. An empty group is not a choice
	// the user can make anyway — it is the shape left behind when every
	// protocol in it was switched off.
	if len(kept) == 0 {
		if tag := m.GetString("tag"); tag != "" && dropped != nil {
			dropped[tag] = true
		}
		return false, true
	}
	// A selector's "default" is a tag like any other: left pointing at a
	// node that was pruned, sing-box refuses the whole config rather than
	// just that group.
	if def := m.GetString("default"); def != "" && !live[def] {
		m.Delete("default")
	}
	return true, changed
}

// getSlice reads an array field, reporting whether it was there and an array.
func getSlice(m *jsonorder.Map, key string) ([]any, bool) {
	v, ok := m.Get(key)
	if !ok {
		return nil, false
	}
	list, ok := v.([]any)
	return list, ok
}

// getMap reads an object field.
func getMap(m *jsonorder.Map, key string) (*jsonorder.Map, bool) {
	v, ok := m.Get(key)
	if !ok {
		return nil, false
	}
	obj, ok := v.(*jsonorder.Map)
	return obj, ok
}

// SetSelectorDefault points the selector at the node the user chose, so a
// restart comes back on it.
//
// Switching at runtime goes through the API and dies with the process; this is
// the half that survives. A choice the config can no longer honour — a node
// switched off since, or one the server dropped — is left alone rather than
// written through, because a "default" naming a tag that is not there is a
// config sing-box refuses to start at all.
func SetSelectorDefault(cfg *jsonorder.Map, node string) {
	node = strings.TrimSpace(node)
	if cfg == nil || node == "" {
		return
	}
	obs, _ := getSlice(cfg, "outbounds")
	for _, o := range obs {
		m, ok := o.(*jsonorder.Map)
		if !ok || m == nil || m.GetString("type") != "selector" {
			continue
		}
		list, _ := getSlice(m, "outbounds")
		for _, v := range list {
			if s, ok := v.(string); ok && s == node {
				m.Set("default", node)
				return
			}
		}
		return
	}
}

// rootKeyOrder is the order sing-box documents its top-level sections in.
//
// The client builds a config by merging a downloaded profile with what only
// this machine knows, and a merge appends: whatever the profile did not carry —
// the TUN inbound, the API service, the cache — landed after everything else,
// in the order the code happened to add it.
var rootKeyOrder = []string{
	"$schema", "log", "dns", "ntp", "certificate", "certificate_providers",
	"http_clients", "network_namespaces", "endpoints", "inbounds", "outbounds",
	"route", "services", "experimental",
}

// SortRoot puts the top-level sections in that order. Only the top level: the
// contents of each section stay in the order the profile's author wrote them.
func SortRoot(cfg *jsonorder.Map) {
	if cfg != nil {
		cfg.SortKeys(rootKeyOrder)
	}
}

// localResolverTag names a dns server that resolves without a tunnel: the
// "local" one a profile carries for the domains it sends straight out. It is
// what a resolver detouring through a removed proxy falls back to.
func localResolverTag(cfg *jsonorder.Map) string {
	dns, ok := getMap(cfg, "dns")
	if !ok {
		return ""
	}
	servers, _ := getSlice(dns, "servers")
	for _, s := range servers {
		m, ok := s.(*jsonorder.Map)
		if !ok || m == nil {
			continue
		}
		if m.GetString("type") != "local" {
			continue
		}
		if tag := m.GetString("tag"); tag != "" {
			return tag
		}
	}
	return ""
}

// directOutboundTag names the outbound that dials without a tunnel.
func directOutboundTag(cfg *jsonorder.Map) string {
	obs, _ := getSlice(cfg, "outbounds")
	for _, o := range obs {
		m, ok := o.(*jsonorder.Map)
		if !ok || m == nil {
			continue
		}
		if m.GetString("type") != "direct" {
			continue
		}
		if tag := m.GetString("tag"); tag != "" {
			return tag
		}
	}
	return ""
}

// HasTunnel reports whether the config still has something to send traffic
// through: a proxy outbound the route reaches through its groups and detours.
// Everything switched off leaves a config that is valid and does nothing —
// sing-box would start it and route every packet straight out, which is not
// what "connect" means. A transport kept only because a resolver, a rule set's
// download or a group nothing routes to still names it is that case too: it is
// in the config, but no traffic goes through it.
func HasTunnel(cfg *jsonorder.Map) bool {
	obs, _ := getSlice(cfg, "outbounds")
	index := map[string]*jsonorder.Map{}
	var first string
	for _, o := range obs {
		if m, ok := o.(*jsonorder.Map); ok && m != nil {
			tag := stringField(m, "tag")
			if first == "" {
				first = tag
			}
			index[tag] = m
		}
	}
	entries := RouteEntries(cfg)
	// Without a final sing-box sends what no rule matched to the first outbound.
	if route, _ := getMap(cfg, "route"); stringField(route, "final") == "" {
		entries = append(entries, first)
	}
	return reachesProxy(index, entries)
}

func reachesProxy(index map[string]*jsonorder.Map, entries []string) bool {
	seen := map[string]bool{}
	var reaches func(string) bool
	reaches = func(tag string) bool {
		m := index[tag]
		if m == nil || seen[tag] {
			return false
		}
		seen[tag] = true
		if proxyLeaf(m) {
			return true
		}
		list, _ := members(m)
		for _, next := range append(list, stringField(m, "detour")) {
			if reaches(next) {
				return true
			}
		}
		return false
	}
	return slices.ContainsFunc(entries, reaches)
}
