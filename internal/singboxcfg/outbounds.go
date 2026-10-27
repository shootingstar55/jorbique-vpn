package singboxcfg

import (
	"fmt"
	"slices"
	"strings"

	"jorbiquevpn/internal/jsonorder"
)

// Which proxy outbounds exist is the server's decision, and the downloaded
// profile is where it says so: a protocol added on the server shows up without
// a client release, and one removed loses its toggle and its place in the
// failover order.

// notSelectableTags are the conventional names for plumbing, for profiles that
// leave the type off.
var notSelectableTags = map[string]struct{}{
	"auto":    {},
	"direct":  {},
	"block":   {},
	"dns-out": {},
}

func outboundMaps(outbounds []any) []*jsonorder.Map {
	out := make([]*jsonorder.Map, 0, len(outbounds))
	for _, o := range outbounds {
		if m, ok := o.(*jsonorder.Map); ok && m != nil {
			out = append(out, m)
		}
	}
	return out
}

func stringField(m *jsonorder.Map, key string) string {
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m.GetString(key))
}

// members lists the tags a group names, and reports whether the outbound is a
// group at all. Anything carrying an "outbounds" list is one, whatever its type
// says, so a group type sing-box adds later is walked rather than offered as a
// node.
func members(m *jsonorder.Map) ([]string, bool) {
	if m == nil {
		return nil, false
	}
	typ := strings.ToLower(stringField(m, "type"))
	list, exists := getSlice(m, "outbounds")
	var tags []string
	for _, item := range list {
		if tag, ok := item.(string); ok && strings.TrimSpace(tag) != "" {
			tags = append(tags, strings.TrimSpace(tag))
		}
	}
	return tags, exists || typ == "urltest" || typ == "selector"
}

func indexOutbounds(outbounds []*jsonorder.Map) map[string]*jsonorder.Map {
	index := make(map[string]*jsonorder.Map, len(outbounds))
	for _, m := range outbounds {
		if tag := stringField(m, "tag"); tag != "" {
			index[tag] = m
		}
	}
	return index
}

// proxyLeaf reports whether an outbound carries traffic itself rather than
// routing it somewhere else.
func proxyLeaf(m *jsonorder.Map) bool {
	if _, group := members(m); group {
		return false
	}
	switch typ := strings.ToLower(stringField(m, "type")); typ {
	case "direct", "block", "dns":
		return false
	case "":
		_, plumbing := notSelectableTags[strings.ToLower(stringField(m, "tag"))]
		return !plumbing
	}
	return true
}

// failoverRoot is the group whose order the profile means the proxies to be
// tried in: "auto" by name, else the first urltest.
func failoverRoot(outbounds []*jsonorder.Map, index map[string]*jsonorder.Map) string {
	if _, group := members(index["auto"]); group {
		return "auto"
	}
	for _, m := range outbounds {
		if strings.EqualFold(stringField(m, "type"), "urltest") {
			return stringField(m, "tag")
		}
	}
	return ""
}

// ProxyTags lists the tags a user can choose between, in the order the profile
// means them to be tried: the failover group's own list, nested groups
// expanded in place, then anything selectable it did not mention in profile
// order. A transport that is only ever a detour is not offered: it carries the
// server's credential, not the user's.
//
// Such a transport is offered only when a group the traffic can reach names
// it: one reachable from the failover group or from entries, the route's own
// targets (RouteEntries of the profile). A leftover group nothing routes to
// cannot promote it, or the user's credential would go into the transport
// every proxy detouring through it shares.
func ProxyTags(outbounds []any, entries []string) []string {
	obs := outboundMaps(outbounds)
	index := indexOutbounds(obs)
	detours := map[string]bool{}
	for _, m := range obs {
		detours[stringField(m, "detour")] = true
	}
	root := failoverRoot(obs, index)
	candidates := reachableCandidates(index, append([]string{root}, entries...))
	selectable := map[string]bool{}
	for _, m := range obs {
		tag := stringField(m, "tag")
		if tag == "" || !proxyLeaf(m) || (detours[tag] && !candidates[tag]) {
			continue
		}
		selectable[tag] = true
	}
	return orderedProxyTags(obs, index, selectable, root)
}

func orderedProxyTags(obs []*jsonorder.Map, index map[string]*jsonorder.Map, selectable map[string]bool, root string) []string {
	var tags []string
	seen := map[string]bool{}
	var visit func(string)
	visit = func(tag string) {
		if seen[tag] || index[tag] == nil {
			return
		}
		seen[tag] = true
		if selectable[tag] {
			tags = append(tags, tag)
			return
		}
		list, _ := members(index[tag])
		for _, member := range list {
			visit(member)
		}
	}
	if root != "" {
		visit(root)
	}
	for _, m := range obs {
		if tag := stringField(m, "tag"); selectable[tag] {
			visit(tag)
		}
	}
	return tags
}

func reachableCandidates(index map[string]*jsonorder.Map, entries []string) map[string]bool {
	reached, candidates := map[string]bool{}, map[string]bool{}
	var reach func(string)
	reach = func(tag string) {
		if reached[tag] || index[tag] == nil {
			return
		}
		reached[tag] = true
		list, _ := members(index[tag])
		for _, member := range list {
			candidates[member] = true
			reach(member)
		}
	}
	for _, entry := range entries {
		reach(entry)
	}
	return candidates
}

// FilterOutbounds takes out the proxies the user switched off and trims every
// group to what is left, without flattening nested groups: a urltest over two
// regional groups stays a urltest over two regional groups. An answer missing
// from enabled means on, so a protocol the server adds is used until the user
// says otherwise, and one that is not reachable from "auto" is added to it.
//
// A group left with nothing in it stays in the array, emptied.
// PruneDanglingReferences is what drops the empty group, along with the
// routes and resolvers that named it; it needs to see the group go to know a
// resolver that dialled through it can dial for itself.
//
// root is the profile the outbounds come from, or nil: its route decides which
// groups may offer a transport, and whatever its route, rule sets or resolvers
// name is kept.
func FilterOutbounds(outbounds []any, root *jsonorder.Map, enabled map[string]bool) ([]any, error) {
	obs := outboundMaps(outbounds)
	index := indexOutbounds(obs)
	available := ProxyTags(outbounds, RouteEntries(root))
	selectable, retained := map[string]bool{}, map[string]bool{}
	for tag := range index {
		retained[tag] = true
	}
	var active []string
	for _, tag := range available {
		selectable[tag] = true
		if on, answered := enabled[tag]; answered && !on {
			retained[tag] = false
		} else {
			active = append(active, tag)
		}
	}
	original := map[string][]string{}
	for _, m := range obs {
		if list, group := members(m); group {
			original[stringField(m, "tag")] = list
		}
	}
	processed, visiting := map[string]bool{}, map[string]bool{}

	for _, m := range obs {
		filterOutboundGroup(stringField(m, "tag"), index, retained, processed, visiting, active)
	}
	revived := reviveDetourTransports(obs, index, retained, original)
	for _, tag := range RouteEntries(root) {
		if revived[tag] {
			return nil, fmt.Errorf("the route sends traffic to %q, which is turned off", tag)
		}
	}
	pruneUnusedDetours(obs, index, selectable, retained, root)
	filtered, err := retainedOutbounds(outbounds, retained, revived)
	if err != nil {
		return nil, err
	}
	if on := detourCycle(outboundMaps(filtered)); on != "" {
		return nil, fmt.Errorf("outbound %q is on a detour cycle", on)
	}
	return filtered, nil
}

// reviveDetourTransports brings back an outbound the user turned off that a
// kept outbound still detours through, directly or through a group, and
// returns what it brought back. It comes back as that transport only: in no
// group, so it is never picked on its own. A group the detour goes through
// that turning it off emptied comes back with all its members, this one
// included, and can pick it again. Failing the build instead stopped every
// start over an ordinary toggle, and the user had no way to see which
// outbound was behind it.
//
// The walk runs in profile order: which edge counts as the back one depends on
// where it starts, and the same profile must always build the same config.
func reviveDetourTransports(obs []*jsonorder.Map, index map[string]*jsonorder.Map, retained map[string]bool, original map[string][]string) map[string]bool {
	revived, walking := map[string]bool{}, map[string]bool{}
	var revive func(string)
	revive = func(tag string) {
		m := index[tag]
		if m == nil || retained[tag] || walking[tag] {
			return
		}
		retained[tag], revived[tag], walking[tag] = true, true, true
		defer delete(walking, tag)
		revive(stringField(m, "detour"))
		list, group := original[tag]
		if !group {
			return
		}
		for _, member := range list {
			revive(member)
		}
		// A member still being walked is a back edge: the cycle
		// filterOutboundGroup dropped, which sing-box refuses to start on.
		kept := keptMembers(list, retained, walking)
		setGroupMembers(m, kept)
		retained[tag] = len(kept) > 0
	}
	for _, m := range obs {
		if retained[stringField(m, "tag")] {
			revive(stringField(m, "detour"))
		}
	}
	return revived
}

// keptMembers is list without the members turned off, the ones still being
// walked, and the repeats.
func keptMembers(list []string, retained, walking map[string]bool) []string {
	var kept []string
	for _, member := range list {
		if retained[member] && !walking[member] && !slices.Contains(kept, member) {
			kept = append(kept, member)
		}
	}
	return kept
}

// detourCycle names an outbound on a dependency cycle, through detours and
// group members, or returns "". sing-box refuses to start on one; saying so
// here names the outbound instead of leaving sing-box to fail later.
func detourCycle(obs []*jsonorder.Map) string {
	index, state := indexOutbounds(obs), map[string]int{}
	var walk func(string) string
	walk = func(tag string) string {
		if state[tag] == 1 {
			return tag
		}
		if state[tag] == 2 || index[tag] == nil {
			return ""
		}
		state[tag] = 1
		deps, _ := members(index[tag])
		for _, dep := range append(deps, stringField(index[tag], "detour")) {
			if on := walk(dep); on != "" {
				return on
			}
		}
		state[tag] = 2
		return ""
	}
	for _, m := range obs {
		if on := walk(stringField(m, "tag")); on != "" {
			return on
		}
	}
	return ""
}

func retainedOutbounds(outbounds []any, retained, revived map[string]bool) ([]any, error) {
	filtered, transports := []any{}, []any{}
	emitted := map[string]bool{}
	for _, o := range outbounds {
		m, ok := o.(*jsonorder.Map)
		if !ok || m == nil {
			filtered = append(filtered, o)
			continue
		}
		tag := stringField(m, "tag")
		if _, group := members(m); tag != "" && !retained[tag] && !group {
			continue
		}
		if tag != "" && emitted[tag] {
			return nil, fmt.Errorf("outbound tag %q is used more than once in the profile", tag)
		}
		emitted[tag] = true
		if detour := stringField(m, "detour"); detour != "" && !retained[detour] {
			return nil, fmt.Errorf("outbound %q detour references missing, disabled or empty outbound %q", tag, detour)
		}
		if revived[tag] {
			// Last, so a profile without route.final cannot default to it.
			transports = append(transports, o)
			continue
		}
		filtered = append(filtered, o)
	}
	return append(filtered, transports...), nil
}

func pruneUnusedDetours(obs []*jsonorder.Map, index map[string]*jsonorder.Map, selectable, retained map[string]bool, root *jsonorder.Map) {
	// A detour-only helper goes with its last consumer, or it would be offered
	// as a proxy of its own once nothing uses it. Whatever still names it is a
	// consumer: a detour, a group, and the profile's route, rule sets and
	// resolvers. Pruning it from under those took the rule or the resolver with
	// it, or left the rule set dangling. Kept, it still is no tunnel unless the
	// route reaches it: HasTunnel walks from the route, not the list.
	helper, needed := map[string]bool{}, map[string]bool{}
	for _, m := range obs {
		if tag := stringField(m, "detour"); index[tag] != nil && !selectable[tag] && proxyLeaf(index[tag]) {
			helper[tag] = true
		}
	}

	for tag := range index {
		if !helper[tag] {
			markNeededOutbound(tag, index, retained, needed)
		}
	}
	for _, tag := range references(root, true) {
		markNeededOutbound(tag, index, retained, needed)
	}
	for tag := range helper {
		if !needed[tag] {
			retained[tag] = false
		}
	}
}

func markNeededOutbound(tag string, index map[string]*jsonorder.Map, retained, needed map[string]bool) {
	if needed[tag] || !retained[tag] {
		return
	}
	needed[tag] = true
	markNeededOutbound(stringField(index[tag], "detour"), index, retained, needed)
	list, _ := members(index[tag])
	for _, member := range list {
		markNeededOutbound(member, index, retained, needed)
	}
}

func filterOutboundGroup(tag string, index map[string]*jsonorder.Map, retained, processed, visiting map[string]bool, active []string) {
	if !retained[tag] || processed[tag] || visiting[tag] {
		return
	}
	m := index[tag]
	list, group := members(m)
	if !group {
		return
	}
	visiting[tag] = true
	var kept []string
	for _, child := range list {
		filterOutboundGroup(child, index, retained, processed, visiting, active)
		// A child still being visited closes a cycle. sing-box cannot start
		// on one, and failing here would take down every connect for a typo
		// in a group nobody uses, so the back edge is dropped instead.
		if retained[child] && !visiting[child] && !slices.Contains(kept, child) {
			kept = append(kept, child)
		}
	}
	if tag == "auto" {
		kept = includeActiveProxies(index, kept, active)
	}
	setGroupMembers(m, kept)
	retained[tag] = len(kept) > 0
	visiting[tag] = false
	processed[tag] = true
}

func includeActiveProxies(index map[string]*jsonorder.Map, kept, active []string) []string {
	reachable := map[string]bool{}
	var visit func(string)
	visit = func(child string) {
		if reachable[child] {
			return
		}
		reachable[child] = true
		children, _ := members(index[child])
		for _, nested := range children {
			visit(nested)
		}
	}
	for _, child := range kept {
		visit(child)
	}
	for _, child := range active {
		if !reachable[child] {
			kept = append(kept, child)
		}
	}

	return kept
}

func setGroupMembers(m *jsonorder.Map, kept []string) {
	values := make([]any, len(kept))
	for i, child := range kept {
		values[i] = child
	}
	m.Set("outbounds", values)
	if def := stringField(m, "default"); def != "" && len(kept) > 0 && !slices.Contains(kept, def) {
		m.Set("default", kept[0])
	}
}

// references lists the outbounds the profile names from outside the outbounds
// array: route.final and every route rule's outbound, nested logical rules
// included. With detours it adds what fetches through one without routing
// traffic to it: each rule set's download_detour and each DNS server's detour.
func references(root *jsonorder.Map, detours bool) []string {
	if root == nil {
		return nil
	}
	var tags []string
	add := func(m *jsonorder.Map, key string) {
		if tag := stringField(m, key); tag != "" {
			tags = append(tags, tag)
		}
	}

	route, _ := getMap(root, "route")
	if route != nil {
		add(route, "final")
		list, _ := getSlice(route, "rules")
		visitRouteRules(list, func(m *jsonorder.Map) { add(m, "outbound") })
	}
	if !detours {
		return tags
	}
	if route != nil {
		sets, _ := getSlice(route, "rule_set")
		for _, set := range sets {
			m, _ := set.(*jsonorder.Map)
			add(m, "download_detour")
		}
	}
	if dns, _ := getMap(root, "dns"); dns != nil {
		servers, _ := getSlice(dns, "servers")
		for _, server := range servers {
			m, _ := server.(*jsonorder.Map)
			add(m, "detour")
		}
	}
	return tags
}

func visitRouteRules(list []any, visit func(*jsonorder.Map)) {
	for _, r := range list {
		m, ok := r.(*jsonorder.Map)
		if !ok || m == nil {
			continue
		}
		visit(m)
		nested, _ := getSlice(m, "rules")
		visitRouteRules(nested, visit)
	}
}

// RouteEntries lists the outbounds the route sends traffic to: route.final and
// each rule's outbound, nested logical rules included. It is the entries
// argument of ProxyTags.
func RouteEntries(root *jsonorder.Map) []string { return references(root, false) }

// ApplyCredential puts the user's credential into every field the profile left
// empty for it, and returns how many it filled. The empty string is the
// placeholder, and the field it sits in is whatever the protocol calls its
// credential: uuid for vless, password for anytls, userkey for snell.
// Naming the fields here meant a client release before a new outbound could
// authenticate at all, so nothing is named. A field the server filled is its
// own: Snell ships a shared psk beside the per-user userkey, and only the
// userkey is left empty.
//
// New profiles mark exact fields with CredentialPlaceholder, including nested
// fields. Empty top-level fields remain supported for previously served profiles.
// An empty nested TLS field is never treated as a credential.
const CredentialPlaceholder = "__JORBIQUE_USER_UUID__"

// notCredential names the outbound fields that are never the credential but
// can be shipped empty: empty there asks for sing-box's default (no flow, both
// networks, no detour), and the user's credential in its place breaks the
// outbound with no build error to say so. Only the fallback for unmarked
// profiles reads it.
var notCredential = map[string]struct{}{
	"type": {}, "tag": {}, "server": {}, "detour": {}, "bind_interface": {},
	"domain_strategy": {}, "domain_resolver": {}, "network": {}, "flow": {},
	"packet_encoding": {}, "security": {}, "method": {}, "plugin": {},
	"plugin_opts": {}, "congestion_control": {}, "udp_relay_mode": {},
}

func ApplyCredential(m *jsonorder.Map, value string) int {
	_, filled := applyExplicitCredential(m, value)
	if filled > 0 {
		// A marked outbound uses the explicit contract exclusively. Even an
		// accidentally empty server key/flow must never receive the user's UUID.
		return filled
	}
	for _, key := range m.Keys() {
		if _, skip := notCredential[key]; skip {
			continue
		}
		if v, _ := m.Get(key); v == "" {
			m.Set(key, value)
			filled++
		}
	}
	return filled
}

func applyExplicitCredential(raw any, credential string) (any, int) {
	filled := 0
	switch v := raw.(type) {
	case string:
		if v == CredentialPlaceholder {
			return credential, 1
		}
	case *jsonorder.Map:
		for _, key := range v.Keys() {
			field, _ := v.Get(key)
			replaced, count := applyExplicitCredential(field, credential)
			v.Set(key, replaced)
			filled += count
		}
	case []any:
		for i, field := range v {
			replaced, count := applyExplicitCredential(field, credential)
			v[i] = replaced
			filled += count
		}
	}
	return raw, filled
}

// CheckProfile runs the checks a build runs on the profile itself, with
// nothing turned off: a proxy outbound to connect through, unique tags, valid
// detours, no detour cycle, and somewhere in the proxies to put the user's
// credential. It returns the first problem, or nil. A profile that fails here
// fails every build, whatever the user chose, so it must not replace a cached
// copy that builds. FilterOutbounds and ApplyCredential rewrite what they are
// given, so root must come from a parse of its own.
func CheckProfile(root *jsonorder.Map) error {
	outbounds, _ := getSlice(root, "outbounds")
	for i, outbound := range outbounds {
		if m, ok := outbound.(*jsonorder.Map); !ok || m == nil {
			return fmt.Errorf("outbound %d is not an object", i)
		}
	}
	proxies := ProxyTags(outbounds, RouteEntries(root))
	if len(proxies) == 0 {
		return fmt.Errorf("the profile has no proxy outbounds")
	}
	if _, err := FilterOutbounds(outbounds, root, nil); err != nil {
		return err
	}
	index := indexOutbounds(outboundMaps(outbounds))
	filled := 0
	for _, tag := range proxies {
		filled += ApplyCredential(index[tag], "profile-check")
	}
	if filled == 0 {
		return fmt.Errorf("the profile leaves no field in any outbound to put the user's credential in")
	}
	return nil
}
