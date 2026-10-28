//go:build darwin

package ui

import (
	"os"
	"path/filepath"
	"strings"
)

// Every window is served from its own loopback port, so each one is a separate
// browser origin with a localStorage of its own: a theme picked in the main
// window was invisible to the panels, which opened in whatever the system said.
//
// The choice lives here instead, and every window is opened with it in its URL.
const (
	themeModeDefault = "dark"
	themeFileName    = "theme"
)

var themeModes = map[string]bool{"dark": true, "light": true}

// themeFilePath is under the user's own config directory: the menu bar runs as
// the user, and this is their preference, not the daemon's. A variable so a test
// can point it somewhere else — os.UserConfigDir ignores the environment on
// macOS, so there is nothing else to redirect.
var themeFilePath = func() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "JorbiqueVPN", themeFileName)
}

// readThemeMode returns the stored light/dark choice, defaulting to dark.
func readThemeMode() string {
	path := themeFilePath()
	if path == "" {
		return themeModeDefault
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return themeModeDefault
	}
	mode := strings.ToLower(strings.TrimSpace(string(data)))
	if !themeModes[mode] {
		return themeModeDefault
	}
	return mode
}

// writeThemeMode remembers the mode for the next window. Best-effort: a theme
// that fails to persist still applies to the windows that are open.
func writeThemeMode(mode string) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if !themeModes[mode] {
		return
	}
	path := themeFilePath()
	if path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(mode), 0o644)
}

// panelQuery is the query string a window is opened with: which page to draw,
// and the theme every window has to agree on.
func panelQuery(page string) string {
	return page + "=1&theme=" + readThemeMode()
}
