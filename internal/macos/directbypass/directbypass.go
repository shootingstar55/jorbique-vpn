package directbypass

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var safariBundleIDs = map[string]struct{}{
	"com.apple.Safari":                  {},
	"com.apple.SafariTechnologyPreview": {},
}

var webKitNetworkingCandidates = []string{
	"/System/Volumes/Preboot/Cryptexes/OS/System/Library/Frameworks/WebKit.framework/Versions/A/XPCServices/com.apple.WebKit.Networking.xpc/Contents/MacOS/com.apple.WebKit.Networking",
	"/System/Cryptexes/OS/System/Library/Frameworks/WebKit.framework/Versions/A/XPCServices/com.apple.WebKit.Networking.xpc/Contents/MacOS/com.apple.WebKit.Networking",
	"/System/Library/Frameworks/WebKit.framework/Versions/A/XPCServices/com.apple.WebKit.Networking.xpc/Contents/MacOS/com.apple.WebKit.Networking",
}

// ExpandedPathsForExecutable expands an app bundle to every executable in it,
// and leaves any other path alone.
func ExpandedPathsForExecutable(p string) []string {
	t := strings.TrimSpace(p)
	if t == "" {
		return nil
	}
	if root := appBundleRoot(t); root != "" {
		return expandedPathsForAppBundle(root)
	}
	return []string{t}
}

func expandedPathsForAppBundle(bundleURL string) []string {
	paths := collectMacOSExecutablesUnderAppBundle(bundleURL)
	bundleID := readBundleIdentifier(bundleURL)
	if shouldAttachWebKitNetworking(bundleID, paths) {
		for p := range existingWebKitNetworkingPaths() {
			paths[p] = struct{}{}
		}
	}
	out := make([]string, 0, len(paths))
	for p := range paths {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func appBundleRoot(executablePath string) string {
	t := strings.TrimSpace(executablePath)
	if t == "" {
		return ""
	}
	st, err := os.Stat(t)
	if err == nil && st.IsDir() && strings.ToLower(filepath.Ext(t)) == ".app" {
		return t
	}
	dir := t
	if err == nil && !st.IsDir() {
		dir = filepath.Dir(dir)
	} else if err != nil {
		dir = filepath.Dir(dir)
	}
	outermost := ""
	for dir != "/" && dir != "" {
		if strings.ToLower(filepath.Ext(dir)) == ".app" {
			outermost = dir
		}
		dir = filepath.Dir(dir)
	}
	return outermost
}

func collectMacOSExecutablesUnderAppBundle(bundleURL string) map[string]struct{} {
	out := map[string]struct{}{}
	contents := filepath.Join(bundleURL, "Contents")
	if _, err := os.Stat(contents); err != nil {
		return out
	}
	_ = filepath.WalkDir(contents, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Base(filepath.Dir(path)) != "MacOS" {
			return nil
		}
		st, err := os.Stat(path)
		if err != nil {
			return nil
		}
		if st.Mode()&0o111 == 0 {
			return nil
		}
		out[path] = struct{}{}
		return nil
	})
	return out
}

func readBundleIdentifier(bundleURL string) string {
	infoPath := filepath.Join(bundleURL, "Contents", "Info.plist")
	if _, err := os.Stat(infoPath); err != nil {
		return ""
	}
	return plutilGetStringKey(infoPath, "CFBundleIdentifier")
}

func shouldAttachWebKitNetworking(bundleID string, collected map[string]struct{}) bool {
	if bundleID != "" {
		if _, ok := safariBundleIDs[bundleID]; ok {
			return true
		}
	}
	for p := range collected {
		if strings.Contains(strings.ToLower(p), "safari.app") {
			return true
		}
	}
	return false
}

func existingWebKitNetworkingPaths() map[string]struct{} {
	out := map[string]struct{}{}
	for _, p := range webKitNetworkingCandidates {
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			continue
		}
		if st.Mode()&0o111 == 0 {
			continue
		}
		out[p] = struct{}{}
	}
	return out
}

// plutilGetStringKey calls plutil to extract a plist string.
func plutilGetStringKey(infoPath string, key string) string {
	// Fast path: plutil -convert json + parse
	out := runReadCmd("/usr/bin/plutil", "-convert", "json", "-o", "-", infoPath)
	if out == "" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
