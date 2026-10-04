//go:build darwin

package ui

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"jorbiquevpn/internal/macos/core"
)

// routeRulesInput is the editable state of the route-rules panel. Domain fields
// are comma-separated strings as shown; Paths is the flat app-bypass executable
// list. Processes is passed through unchanged (the panel does not edit it),
// matching ui.m.
type routeRulesInput struct {
	Direct, Processes string
	Paths             []string
}

// routeRulesFromConfig extracts the current rules from the config. Ports the
// cfg-reading half of cmdRouteRules.
func routeRulesFromConfig(cfg *core.AppConfig) routeRulesInput {
	return routeRulesInput{
		Direct:    strings.Join(cfg.DirectDomains, ", "),
		Processes: strings.Join(cfg.DirectBypassProcesses, ", "),
		Paths:     cfg.DirectBypassPaths,
	}
}

// routeRulesStdin builds the `set-route-rules` stdin: three newline-separated
// lines (direct, processes, tab-joined paths).
func routeRulesStdin(r routeRulesInput) string {
	fields := []string{r.Direct, r.Processes}
	// Textareas allow line breaks; the CLI reserves them for field boundaries.
	lines := strings.NewReplacer("\r\n", ",", "\r", ",", "\n", ",")
	for i := range fields {
		fields[i] = lines.Replace(fields[i])
	}
	return strings.Join(append(fields, strings.Join(r.Paths, "\t")), "\n")
}

// ---------------------------------------------------------------------------
// App-bypass grouping (pure path logic, ported from uiGroupsFromFlatPaths).
// ---------------------------------------------------------------------------

// appBypassGroup is one app's bypass executables, keyed by its bundle path.
type appBypassGroup struct {
	groupID     string
	displayName string
	paths       []string // sorted, unique
}

// outermostAppBundle returns the outermost ancestor of p (or p itself) ending in
// ".app", or "" when none. Pure path walk.
func outermostAppBundle(p string) string {
	var outer string
	cur := p
	for cur != "/" && cur != "" && cur != "." {
		if strings.EqualFold(filepath.Ext(cur), ".app") {
			outer = cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return outer
}

// groupsFromFlatPaths groups bypass paths by their owning .app bundle, attaching
// WebKit-networking orphans to the Safari group when present, and turning other
// non-bundle paths into single-path groups. Sorted by display name. Ports
// uiGroupsFromFlatPaths.
func groupsFromFlatPaths(flat []string) []appBypassGroup {
	byBundle, orphans := splitBundlePaths(flat)

	var groups []appBypassGroup
	keys := make([]string, 0, len(byBundle))
	for k := range byBundle {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, bp := range keys {
		groups = append(groups, appBypassGroup{groupID: bp, displayName: appDisplayName(bp), paths: sortedUnique(byBundle[bp])})
	}

	var webkit, other []string
	for _, o := range orphans {
		if strings.Contains(strings.ToLower(o), "com.apple.webkit.networking") {
			webkit = append(webkit, o)
		} else {
			other = append(other, o)
		}
	}
	if safariIdx := indexOfSafari(groups); safariIdx >= 0 && len(webkit) > 0 {
		groups[safariIdx].paths = sortedUnique(append(groups[safariIdx].paths, webkit...))
	} else if len(webkit) > 0 {
		groups = append(groups, appBypassGroup{
			groupID: "__webkit_networking__", displayName: "WebKit networking (system)", paths: sortedUnique(webkit),
		})
	}
	sort.Strings(other)
	for _, o := range other {
		groups = append(groups, appBypassGroup{groupID: o, displayName: filepath.Base(o), paths: []string{o}})
	}
	sort.SliceStable(groups, func(i, j int) bool {
		return strings.ToLower(groups[i].displayName) < strings.ToLower(groups[j].displayName)
	})
	return groups
}

func splitBundlePaths(flat []string) (map[string][]string, []string) {
	byBundle := map[string][]string{}
	var orphans []string
	for _, raw := range flat {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		base := p
		if fi, err := os.Stat(base); err == nil && !fi.IsDir() {
			base = filepath.Dir(base)
		}
		outer := outermostAppBundle(base)
		if outer == "" {
			outer = outermostAppBundle(p)
		}
		if outer != "" {
			byBundle[outer] = append(byBundle[outer], p)
		} else {
			orphans = append(orphans, p)
		}
	}

	return byBundle, orphans
}

func indexOfSafari(groups []appBypassGroup) int {
	for i, g := range groups {
		if strings.Contains(strings.ToLower(g.groupID), "safari.app") {
			return i
		}
	}
	return -1
}

// appDisplayName derives a human name from a bundle path (its basename without
// the .app extension). ui.m additionally reads CFBundleDisplayName from
// Info.plist; the basename is a faithful-enough fallback for the list label.
func appDisplayName(bundlePath string) string {
	base := filepath.Base(bundlePath)
	if ext := filepath.Ext(base); strings.EqualFold(ext, ".app") {
		return strings.TrimSuffix(base, ext)
	}
	return base
}

func sortedUnique(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// Filesystem expansion of a chosen .app (ports uiExpandedPathsForChosenURL).
// ---------------------------------------------------------------------------

var webKitNetworkingCandidates = []string{
	"/System/Volumes/Preboot/Cryptexes/OS/System/Library/Frameworks/WebKit.framework/Versions/A/XPCServices/com.apple.WebKit.Networking.xpc/Contents/MacOS/com.apple.WebKit.Networking",
	"/System/Cryptexes/OS/System/Library/Frameworks/WebKit.framework/Versions/A/XPCServices/com.apple.WebKit.Networking.xpc/Contents/MacOS/com.apple.WebKit.Networking",
	"/System/Library/Frameworks/WebKit.framework/Versions/A/XPCServices/com.apple.WebKit.Networking.xpc/Contents/MacOS/com.apple.WebKit.Networking",
}

// expandedPathsForChosenApp returns the executables to bypass for a chosen path:
// for a .app, every Contents/**/MacOS/* executable (plus system WebKit for
// Safari); for anything else, the path itself.
func expandedPathsForChosenApp(path string) []string {
	fi, err := os.Stat(path)
	if err != nil {
		return []string{path}
	}
	if fi.IsDir() && strings.EqualFold(filepath.Ext(path), ".app") {
		set := map[string]bool{}
		for _, e := range collectMacOSExecutables(path) {
			set[e] = true
		}
		if strings.Contains(strings.ToLower(path), "safari.app") {
			for _, w := range existingExecutables(webKitNetworkingCandidates) {
				set[w] = true
			}
		}
		keys := make([]string, 0, len(set))
		for k := range set {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return keys
	}
	return []string{path}
}

// collectMacOSExecutables walks a bundle's Contents for executable files whose
// immediate parent directory is named "MacOS". Ports uiCollectMacOSExecutables.
func collectMacOSExecutables(bundlePath string) []string {
	var out []string
	contents := filepath.Join(bundlePath, "Contents")
	_ = filepath.WalkDir(contents, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if filepath.Base(filepath.Dir(p)) != "MacOS" {
			return nil
		}
		if fi, e := os.Stat(p); e == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// existingExecutables filters paths to those that exist as executable files.
func existingExecutables(paths []string) []string {
	var out []string
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			out = append(out, p)
		}
	}
	return out
}
