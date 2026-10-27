package routerules

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"jorbiquevpn/internal/jsonorder"
	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/macos/fsatomic"
	"jorbiquevpn/internal/macos/singbox"
)

var defaultDirectBypassProcessNames = []string{}

var (
	singboxObjectBraceRe      = regexp.MustCompile(`("singbox"\s*:\s*\{)(\s*)`)
	rulesTopLevelSectionRoute = regexp.MustCompile(`"route"\s*:\s*\{`)
	rulesTopLevelSectionDNS   = regexp.MustCompile(`"dns"\s*:\s*\{`)
	rulesSubKeyOpenBracket    = regexp.MustCompile(`"rules"\s*:\s*\[`)
	// topLevelSingboxSectionRe matches the sing-box *section* — the name, a
	// colon, an object — and so cannot match the string "singbox" appearing as a
	// VPN entry's "type" value.
	topLevelSingboxSectionRe = regexp.MustCompile(`"singbox"\s*:\s*\{`)
)

func singboxSectionHasUUIDKey(data []byte) bool {
	return singboxSectionHasKey(data, "uuid")
}

// singboxSectionHasKey reports whether the singbox section names key; false for
// a file it cannot read.
func singboxSectionHasKey(data []byte, key string) bool {
	var raw map[string]any
	if err := json.Unmarshal(core.StripTrailingCommas(data), &raw); err != nil {
		return false
	}
	sb, ok := raw["singbox"].(map[string]any)
	if !ok {
		return false
	}
	_, has := sb[key]
	return has
}

func supplementalBypassProcessNames(paths []string) []string {
	hasChrome := false
	for _, p := range paths {
		if strings.Contains(strings.ToLower(p), "/google chrome.app/") {
			hasChrome = true
			break
		}
	}
	if !hasChrome {
		return nil
	}
	return []string{
		"Google Chrome",
		"Google Chrome Helper",
		"Google Chrome Helper (GPU)",
		"Google Chrome Helper (Renderer)",
		"Google Chrome Helper (Plugin)",
		"Google Chrome Helper (Alerts)",
	}
}

// ParseDomainList splits comma-separated input into trimmed non-empty list.
func ParseDomainList(input string) []string {
	out := []string{}
	for _, p := range strings.Split(input, ",") {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// PatchVpnCfgRouteRules stores the direct domains and app bypass rules.
func PatchVpnCfgRouteRules(path string, direct, processes, paths []string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	raw := jsonorder.New()
	if err := jsonorder.Unmarshal(core.StripTrailingCommas(data), raw); err != nil {
		return core.BadConfig("invalid vpn_cfg.json: " + err.Error())
	}
	raw.Set("direct_domains", direct)
	raw.Set("direct_bypass_processes", processes)
	raw.Set("direct_bypass_paths", paths)
	data, err = jsonorder.MarshalIndent(raw)
	if err != nil {
		return err
	}
	return fsatomic.WriteFile(path, data, 0o644)
}

// SingboxConfigPathsForRoutePatch returns absolute paths to sing-box JSONs for patching.
func SingboxConfigPathsForRoutePatch(paths *core.Paths, cfg *core.AppConfig) []string {
	var out []string
	for _, v := range cfg.VPNs {
		if v.Type != core.VPNSingbox {
			continue
		}
		if _, err := os.Stat(v.File); err == nil {
			out = append(out, v.File)
		}
	}
	fb := filepath.Join(paths.CfgDir, paths.SingboxFallbackFile)
	if _, err := os.Stat(fb); err == nil {
		found := false
		for _, p := range out {
			if p == fb {
				found = true
				break
			}
		}
		if !found {
			out = append(out, fb)
		}
	}
	return out
}

// ReapplySavedRouteRulesToSingboxFiles re-applies route rules to all sing-box config paths.
func ReapplySavedRouteRulesToSingboxFiles(paths *core.Paths, cfg *core.AppConfig) error {
	for _, p := range SingboxConfigPathsForRoutePatch(paths, cfg) {
		if err := PatchSingboxRouteRules(p, cfg.DirectDomains, cfg.DirectBypassProcesses, cfg.DirectBypassPaths); err != nil {
			return err
		}
	}
	return nil
}

// PatchSingboxRouteRules updates DNS rules and route rules of singbox.json (preserving formatting).
func PatchSingboxRouteRules(path string, directDomains, directBypassProcesses, directBypassPaths []string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var raw map[string]any
	if err := json.Unmarshal(core.StripTrailingCommas(data), &raw); err != nil {
		return fmt.Errorf("Invalid singbox.json")
	}

	dnsRules := buildDNSRules(raw, directDomains)
	routeRules := buildRouteRules(raw, directDomains, directBypassProcesses, directBypassPaths)

	text := string(data)

	type chunk struct {
		start, end int
		s          string
	}
	var chunks []chunk
	for _, section := range []struct {
		key   string
		rules []any
	}{{"route", routeRules}, {"dns", dnsRules}} {
		if len(section.rules) == 0 {
			continue
		}
		start, end := findRulesArrayRangeInTopLevelSection(text, section.key)
		if start < 0 {
			continue
		}
		// Keep the indentation of the surrounding file when replacing an array.
		rendered, err := renderRules(section.rules, indentOfLineAt(text, start))
		if err != nil {
			return err
		}
		chunks = append(chunks, chunk{start, end, string(rendered)})
	}
	sort.Slice(chunks, func(i, j int) bool { return chunks[i].start > chunks[j].start })
	for _, c := range chunks {
		text = text[:c.start] + c.s + text[c.end:]
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

// buildDNSRules carries the profile's DNS rules over and puts the direct names
// in front of them, resolved by the profile's local resolver: a name that goes
// out directly is one the network in front of the machine should answer for.
// The rule this package wrote last time is replaced, not stacked.
func buildDNSRules(raw map[string]any, directDomains []string) []any {
	dns, _ := raw["dns"].(map[string]any)
	existing, ok := dns["rules"].([]any)
	if !ok {
		return nil
	}
	// A rule naming a server this config does not have can never match, so
	// it is dropped — but only when the config lists its servers: with none
	// listed there is nothing to judge a rule against.
	live := liveDNSServers(dns)
	local := localDNSServerTag(raw)
	var dnsRules []any
	for _, rRaw := range existing {
		r, _ := rRaw.(map[string]any)
		server, _ := r["server"].(string)
		if len(live) > 0 && server != "" && !live[server] {
			continue
		}
		if server != "" && server == local && isOwnDNSRule(r) {
			continue
		}
		dnsRules = append(dnsRules, rRaw)
	}
	if local != "" && len(directDomains) > 0 {
		dnsRules = append([]any{dnsRouteRule(directDomains, local)}, dnsRules...)
	}
	return dnsRules
}

// isOwnDNSRule reports whether a DNS rule has the shape dnsRouteRule writes: a
// domain list routed to a resolver and nothing else to match on. A rule with
// any other matcher belongs to the profile and is left alone.
func isOwnDNSRule(r map[string]any) bool {
	if !mapHasAny(r, "domain", "domain_suffix") {
		return false
	}
	for k := range r {
		switch k {
		case "domain", "domain_suffix", "action", "server", "invert":
		default:
			return false
		}
	}
	a, ok := r["action"].(string)
	return !ok || a == "route"
}

func liveDNSServers(dns map[string]any) map[string]bool {
	live := map[string]bool{}
	if servers, ok := dns["servers"].([]any); ok {
		for _, sRaw := range servers {
			if s, ok := sRaw.(map[string]any); ok {
				if tag, _ := s["tag"].(string); tag != "" {
					live[tag] = true
				}
			}
		}
	}
	return live
}

func buildRouteRules(raw map[string]any, directDomains, directBypassProcesses, directBypassPaths []string) []any {
	route, _ := raw["route"].(map[string]any)
	existing, ok := route["rules"].([]any)
	if !ok {
		return nil
	}
	// The profile ships direct-bypass rules of its own — a process the
	// organisation exempts, the corporate zones going direct. They look
	// exactly like the ones written from the Route rules panel, so
	// dropping "rules of that kind" before re-adding ours deleted them
	// and, with an empty panel, put nothing back. Carry what the profile
	// listed into the merged sets instead.
	routeRules, profileProcessNames, profileProcessPaths, ruRegexRuleIdx := retainedRouteRules(existing)

	directDomainsSorted := sortedSetSlice(directDomains)
	directDomainRegex := directDomainRegexList(directDomainsSorted)
	pathSorted := sortedSetSlice(trimSliceNonEmpty(append(append([]string{}, directBypassPaths...), profileProcessPaths...)))
	userProc := uniqueSlice(filterNonEmpty(directBypassProcesses))
	merged := append([]string{}, defaultDirectBypassProcessNames...)
	merged = append(merged, profileProcessNames...)
	merged = append(merged, userProc...)
	merged = append(merged, supplementalBypassProcessNames(pathSorted)...)
	mergedProcessNames := sortedSetSlice(merged)
	if len(directDomainsSorted) > 0 {
		routeRules = append(routeRules, map[string]any{
			"action": "route", "outbound": "direct",
			"domain": stringSliceToAny(directDomainsSorted),
		})
	}
	routeRules = mergeDirectDomainRegex(routeRules, ruRegexRuleIdx, directDomainRegex)
	if len(mergedProcessNames) > 0 {
		routeRules = append(routeRules, map[string]any{
			"action": "route", "outbound": "direct",
			"process_name": stringSliceToAny(mergedProcessNames),
		})
	}
	if len(pathSorted) > 0 {
		routeRules = append(routeRules, map[string]any{
			"action": "route", "outbound": "direct",
			"process_path": stringSliceToAny(pathSorted),
		})
	}
	ensureDirectRouteAction(routeRules)
	return routeRules
}

func retainedRouteRules(existing []any) (routeRules []any, profileProcessNames, profileProcessPaths []string, ruRegexRuleIdx int) {
	ruRegexRuleIdx = -1
	for _, rRaw := range existing {
		r, _ := rRaw.(map[string]any)
		if isOnlyDirectRule(r, "process_name") {
			profileProcessNames = append(profileProcessNames, stringsFromAny(r["process_name"])...)
			continue
		}
		if isOnlyDirectRule(r, "process_path") {
			profileProcessPaths = append(profileProcessPaths, stringsFromAny(r["process_path"])...)
			continue
		}
		// Only our own shape is replaced: outbound direct with a plain
		// domain list and nothing else. The profile's zone rule carries
		// domain_suffix as well, and used to be thrown out with it.
		if isOnlyDirectRule(r, "domain") {
			continue
		}
		routeRules = append(routeRules, rRaw)
		if rr, ok := rRaw.(map[string]any); ok && isRuZoneDirectRegexRule(rr) {
			ruRegexRuleIdx = len(routeRules) - 1
		}
	}

	return
}

func mergeDirectDomainRegex(routeRules []any, ruRegexRuleIdx int, directDomainRegex []string) []any {
	if len(directDomainRegex) == 0 {
		return routeRules
	}
	if ruRegexRuleIdx < 0 {
		return append(routeRules, map[string]any{
			"action": "route", "outbound": "direct", "domain_regex": stringSliceToAny(directDomainRegex),
		})
	}
	if rr, ok := routeRules[ruRegexRuleIdx].(map[string]any); ok {
		rr["domain_regex"] = mergeDomainRegexLists(rr["domain_regex"], directDomainRegex)
	}
	return routeRules
}

func ensureDirectRouteAction(routeRules []any) {
	for _, rRaw := range routeRules {
		r, ok := rRaw.(map[string]any)
		if !ok || r["outbound"] != "direct" {
			continue
		}
		if _, has := r["action"]; has {
			continue
		}
		if _, hasDomain := r["domain"]; hasDomain || mapHasAny(r, "process_name", "process_path") {
			r["action"] = "route"
		}
	}
}

// indentOfLineAt returns the leading whitespace of the line pos falls on, used
// as the prefix for a JSON block spliced in at that position.
func indentOfLineAt(text string, pos int) string {
	if pos < 0 || pos > len(text) {
		return ""
	}
	start := strings.LastIndexByte(text[:pos], '\n') + 1
	end := start
	for end < len(text) && (text[end] == ' ' || text[end] == '\t') {
		end++
	}
	return text[start:end]
}

// localDNSServerTag is the profile's own local resolver — the one that asks the
// system rather than a server reached through the tunnel. A profile without one
// has nothing to point the direct names at.
func localDNSServerTag(raw map[string]any) string {
	dns, ok := raw["dns"].(map[string]any)
	if !ok {
		return ""
	}
	servers, _ := dns["servers"].([]any)
	for _, sRaw := range servers {
		server, ok := sRaw.(map[string]any)
		if !ok {
			continue
		}
		if kind, _ := server["type"].(string); strings.EqualFold(kind, "local") {
			tag, _ := server["tag"].(string)
			return strings.TrimSpace(tag)
		}
	}
	return ""
}

// dnsRouteRule builds one DNS rule in the shape the profile uses: each name as
// an exact `domain` and as a dotted `domain_suffix`. sing-box matches
// domain_suffix literally, so a bare "example.com" would also claim
// "xexample.com"; ".example.com" plus the exact name covers the zone and
// nothing else.
func dnsRouteRule(domains []string, server string) *singbox.OrderedMap {
	exact := make([]string, 0, len(domains))
	suffix := make([]string, 0, len(domains))
	for _, d := range domains {
		d = strings.TrimPrefix(strings.TrimSpace(d), ".")
		if d == "" {
			continue
		}
		exact = append(exact, d)
		suffix = append(suffix, "."+d)
	}
	r := singbox.NewOrderedMap()
	r.Set("domain", stringSliceToAny(exact))
	r.Set("domain_suffix", stringSliceToAny(suffix))
	r.Set("action", "route")
	r.Set("server", server)
	return r
}

func stringSliceToAny(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

func uniqueSlice(in []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, s := range in {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

func filterNonEmpty(in []string) []string {
	out := []string{}
	for _, s := range in {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func trimSliceNonEmpty(in []string) []string {
	out := []string{}
	for _, s := range in {
		t := strings.TrimSpace(s)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

func sortedSetSlice(in []string) []string {
	out := uniqueSlice(in)
	sort.Strings(out)
	return out
}

// isOnlyDirectRule matches a direct rule this package writes: one matcher of
// the given kind and nothing else to match on. A profile rule that also carries
// a domain_suffix, or any other matcher, is not ours to replace.
func isOnlyDirectRule(r map[string]any, matcher string) bool {
	if r["outbound"] != "direct" {
		return false
	}
	if list, ok := r[matcher].([]any); !ok || len(list) == 0 {
		return false
	}
	for k := range r {
		switch k {
		case "action", "outbound", "invert", matcher:
		default:
			return false
		}
	}
	if a, ok := r["action"].(string); ok {
		return a == "route"
	}
	return true
}

// stringsFromAny reads a JSON string array back out of a decoded rule.
func stringsFromAny(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		if s, ok := it.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

func mapHasAny(r map[string]any, keys ...string) bool {
	for _, k := range keys {
		if _, ok := r[k]; ok {
			return true
		}
	}
	return false
}

func directDomainRegexList(domains []string) []string {
	out := make([]string, 0, len(domains))
	for _, d := range domains {
		t := strings.TrimSpace(d)
		if t == "" {
			continue
		}
		out = append(out, `(.*\.)?`+regexp.QuoteMeta(t)+`$`)
	}
	return out
}

func mergeDomainRegexLists(existing any, extra []string) []any {
	seen := map[string]struct{}{}
	out := []any{}
	if arr, ok := existing.([]any); ok {
		for _, v := range arr {
			s, ok := v.(string)
			if !ok {
				continue
			}
			if _, has := seen[s]; has {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	for _, s := range extra {
		if _, has := seen[s]; has {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func isRuZoneDirectRegexRule(r map[string]any) bool {
	if r == nil || r["outbound"] != "direct" {
		return false
	}
	arr, ok := r["domain_regex"].([]any)
	if !ok || len(arr) == 0 {
		return false
	}
	for _, v := range arr {
		s, ok := v.(string)
		if !ok {
			continue
		}
		switch s {
		case `.*\.(ru|su|xn--p1ai)$`, `.*\.ru$`, `.*\.su$`, `.*\.xn--p1ai$`, `.*\.xn--p1ag$`:
			return true
		}
	}
	return false
}

// findRulesArrayRangeInTopLevelSection returns (start, end) of the rules array literal `[...]`
// inside `"<sectionKey>": { ... "rules": [...] ... }`.
func findRulesArrayRangeInTopLevelSection(text, sectionKey string) (int, int) {
	var keyPattern *regexp.Regexp
	switch sectionKey {
	case "route":
		keyPattern = rulesTopLevelSectionRoute
	case "dns":
		keyPattern = rulesTopLevelSectionDNS
	default:
		keyPattern = regexp.MustCompile(`"` + regexp.QuoteMeta(sectionKey) + `"\s*:\s*\{`)
	}
	idx := keyPattern.FindStringIndex(text)
	if idx == nil {
		return -1, -1
	}
	objEnd := jsonContainerEnd(text, idx[1]-1, '{', '}')
	if objEnd < 0 {
		return -1, -1
	}
	rm := rulesSubKeyOpenBracket.FindStringIndex(text[idx[0]:objEnd])
	if rm == nil {
		return -1, -1
	}
	openBracket := idx[0] + rm[1] - 1
	end := jsonContainerEnd(text, openBracket, '[', ']')
	if end < 0 {
		return -1, -1
	}
	return openBracket, end
}

// jsonContainerEnd skips quoted strings so braces and brackets in regexes or
// executable names cannot truncate the JSON block being replaced.
func jsonContainerEnd(text string, start int, open, close byte) int {
	depth := 0
	for i := start; i < len(text); i++ {
		if text[i] == '"' {
			i = jsonStringEnd(text, i)
			if i < 0 {
				return -1
			}
			continue
		}
		switch text[i] {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

func jsonStringEnd(text string, start int) int {
	for i := start + 1; i < len(text); i++ {
		if text[i] == '\\' {
			i++
		} else if text[i] == '"' {
			return i
		}
	}
	return -1
}

// PatchVpnCfgSingboxUUIDAndOutbounds updates singbox.uuid and singbox.enabled_outbounds in vpn_cfg.json.
func PatchVpnCfgSingboxUUIDAndOutbounds(path string, uuid *string, enabled, disabled []string, selectedNode *string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// A file that does not parse is not one to edit blind. It used to be edited
	// anyway: singboxSectionHasUUIDKey answers "no uuid key" for a file it
	// cannot read, so every save appended another uuid/enabled_outbounds pair,
	// wrote the file, and reported success — while the client, using the same
	// parser, ignored the whole overlay and showed an empty panel. Twenty
	// attempts left twenty pairs and no way to tell anything was wrong.
	if err := validVpnCfgJSON(data); err != nil {
		return core.BadConfig("cannot save into " + path + ": " + err.Error() +
			". The file is not valid JSON — restore it from a backup or reinstall the client.")
	}
	content := string(data)

	// Insert keys if missing inside "singbox" object (ignore "uuid" elsewhere in the file).
	if uuid != nil || enabled != nil || disabled != nil {
		content = ensureSingboxChoiceKeys(content, data)
	}

	if uuid != nil {
		content = replaceJSONStringValue("uuid", *uuid, content)
	}
	if enabled != nil {
		content = replaceJSONArrayOfStrings("enabled_outbounds", enabled, content)
	}
	if disabled != nil {
		// A config written by an older client has no such key; add it next to
		// the enabled list rather than losing the user's "no".
		content = updateSingboxChoiceList(content, "disabled_outbounds", "enabled_outbounds", disabled)
	}
	if selectedNode != nil {
		// The node the selector comes up on. Added after whichever list this
		// file already has, so a config written by any older client gains the
		// key rather than losing the choice.
		content = ensureSelectedNodeKey(content)
		content = replaceJSONStringValue("selected_node", *selectedNode, content)
	}

	// And what goes out is checked too. These edits are textual — offsets found
	// by regex — so a mistake in one lands as a file the client can no longer
	// read, and the way that failure showed up was a settings panel that
	// silently forgot everything. Refusing to write leaves the previous file
	// intact and puts the error in front of whoever pressed the button.
	if err := validVpnCfgJSON([]byte(content)); err != nil {
		return core.BadConfig("refusing to write " + path + ": the edit would leave it unreadable (" + err.Error() + ")")
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// PatchVpnCfgSingboxNaiveMode records whether the client connects in the
// NaiveProxy mode. The key goes after the outbound lists, the way selected_node
// does, so a config written by any older client gains it.
func PatchVpnCfgSingboxNaiveMode(path string, on bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := validVpnCfgJSON(data); err != nil {
		return core.BadConfig("cannot save into " + path + ": " + err.Error() +
			". The file is not valid JSON — restore it from a backup or reinstall the client.")
	}
	value := strconv.FormatBool(on)
	content := ensureSingboxChoiceKeys(string(data), data)
	if naiveModeValueRe.MatchString(content) {
		content = naiveModeValueRe.ReplaceAllString(content, "${1}"+value)
	} else {
		content = insertAfterFirstJSONArray(content, `"naive_mode": `+value, "disabled_outbounds", "enabled_outbounds")
	}
	if err := validVpnCfgJSON([]byte(content)); err != nil {
		return core.BadConfig("refusing to write " + path + ": the edit would leave it unreadable (" + err.Error() + ")")
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

var naiveModeValueRe = regexp.MustCompile(`("naive_mode"\s*:\s*)(true|false)`)

func updateSingboxChoiceList(content, key, after string, values []string) string {
	if !strings.Contains(content, `"`+key+`"`) {
		content = insertAfterJSONArray(after, `"`+key+`": []`, content)
	}
	return replaceJSONArrayOfStrings(key, values, content)
}

func ensureSingboxChoiceKeys(content string, data []byte) string {
	if singboxSectionHasUUIDKey(data) {
		return content
	}
	if m := singboxObjectBraceRe.FindStringSubmatchIndex(content); m != nil {
		r2start := m[4]
		r2end := m[5]
		// Only the keys the section lacks: one already there and added again
		// left the object with two of it, and the reader takes the last — the
		// old value — while the edit replaced the first.
		insert := "\n    \"uuid\": \"\","
		for _, key := range []string{"enabled_outbounds", "disabled_outbounds"} {
			if !singboxSectionHasKey(data, key) {
				insert += "\n    \"" + key + "\": [],"
			}
		}
		content = content[:r2start] + insert + content[r2start:r2end] + content[r2end:]
	}
	return content
}

func ensureSelectedNodeKey(content string) string {
	if strings.Contains(content, `"selected_node"`) {
		return content
	}
	return insertAfterFirstJSONArray(content, `"selected_node": ""`, "disabled_outbounds", "enabled_outbounds")
}

// insertAfterFirstJSONArray adds a key after the first of the named arrays the
// file has, so a config written by any older client gains it.
func insertAfterFirstJSONArray(content, newKeyValue string, after ...string) string {
	for _, key := range after {
		if strings.Contains(content, `"`+key+`"`) {
			return insertAfterJSONArray(key, newKeyValue, content)
		}
	}
	return content
}

// validVpnCfgJSON reports whether data parses the way the client reads it —
// trailing commas allowed, since the config has always tolerated them.
func validVpnCfgJSON(data []byte) error {
	var raw map[string]any
	return json.Unmarshal(core.StripTrailingCommas(data), &raw)
}

func replaceJSONStringValue(jsonKey, newValue, content string) string {
	pattern := regexp.MustCompile(`"` + regexp.QuoteMeta(jsonKey) + `"\s*:\s*"`)
	loc := pattern.FindStringIndex(content)
	if loc == nil {
		return content
	}
	keyEnd := loc[1]
	i := keyEnd
	for i < len(content) {
		c := content[i]
		if c == '\\' {
			i += 2
			continue
		}
		if c == '"' {
			break
		}
		i++
	}
	if i >= len(content) {
		return content
	}
	escaped := strings.ReplaceAll(newValue, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return content[:keyEnd] + escaped + content[i:]
}

func replaceJSONArrayOfStrings(jsonKey string, newValue []string, content string) string {
	// encoding/json writes nil slices as null; the first saved choice replaces
	// that value just as it replaces an existing array.
	pattern := regexp.MustCompile(`"` + regexp.QuoteMeta(jsonKey) + `"\s*:\s*(\[|null\b)`)
	loc := pattern.FindStringSubmatchIndex(content)
	if loc == nil {
		return content
	}
	valueStart, i := loc[2], loc[3]
	if content[valueStart] == '[' {
		i = jsonContainerEnd(content, valueStart, '[', ']')
		if i < 0 {
			return content
		}
	}
	items := make([]string, len(newValue))
	for j, v := range newValue {
		s := strings.ReplaceAll(v, `\`, `\\`)
		s = strings.ReplaceAll(s, `"`, `\"`)
		items[j] = `"` + s + `"`
	}
	return content[:valueStart] + "[" + strings.Join(items, ", ") + "]" + content[i:]
}

// insertAfterJSONArray puts a new key right after the array named by jsonKey,
// keeping the indentation of the line the array starts on. Used to add a key an
// older configuration does not have yet.
func insertAfterJSONArray(jsonKey, newKeyValue, content string) string {
	pattern := regexp.MustCompile(`"` + regexp.QuoteMeta(jsonKey) + `"\s*:\s*\[`)
	loc := pattern.FindStringIndex(content)
	if loc == nil {
		return content
	}
	i := jsonContainerEnd(content, loc[1]-1, '[', ']')
	if i < 0 {
		return content
	}
	// The leading whitespace of the line the array is on, so the new key lines
	// up with it. Anything else on that line is not indentation: taking the
	// whole prefix duplicated the rest of the object.
	lineStart := strings.LastIndex(content[:loc[0]], "\n") + 1
	indent := content[lineStart : lineStart+leadingSpaceLen(content[lineStart:])]
	insert := ",\n" + indent + newKeyValue
	// The comma that already follows the array, if any, stays after the pair.
	return content[:i] + insert + content[i:]
}

// leadingSpaceLen counts the spaces and tabs a line starts with.
func leadingSpaceLen(s string) int {
	n := 0
	for n < len(s) && (s[n] == ' ' || s[n] == '\t') {
		n++
	}
	return n
}

// renderRules writes a rules array for splicing back into an already-formatted
// file, indented to the line it replaces.
//
// Not encoding/json: that one reorders nothing here, but it escapes <, > and &
// on the way out — a domain_regex or a credential comes back unreadable — and
// this is the one writer left that touched a config the user reads.
func renderRules(rules []any, prefix string) ([]byte, error) {
	body, err := jsonorder.MarshalIndent(rules)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	for i := 1; i < len(lines); i++ {
		lines[i] = prefix + lines[i]
	}
	return []byte(strings.Join(lines, "\n")), nil
}
