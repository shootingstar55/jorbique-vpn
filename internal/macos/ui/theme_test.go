//go:build darwin

package ui

import (
	"os"
	"path/filepath"
	"testing"
)

// Each window is its own browser origin, so the choice cannot live in the page:
// it is stored here and put into every window's URL.
func TestThemeModeRoundTripsAndDefaultsToDark(t *testing.T) {
	dir := t.TempDir()
	previous := themeFilePath
	themeFilePath = func() string { return filepath.Join(dir, "JorbiqueVPN", themeFileName) }
	t.Cleanup(func() { themeFilePath = previous })

	if got := readThemeMode(); got != "dark" {
		t.Errorf("with nothing stored the theme is %q, want dark", got)
	}
	if got := panelQuery("main"); got != "main=1&theme=dark" {
		t.Errorf("query = %q", got)
	}
	if err := os.MkdirAll(filepath.Dir(themeFilePath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(themeFilePath(), []byte("system"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readThemeMode(); got != "dark" {
		t.Errorf("legacy system mode read as %q, want dark", got)
	}

	writeThemeMode("dark")
	if got := readThemeMode(); got != "dark" {
		t.Errorf("stored theme = %q, want dark", got)
	}
	if got := panelQuery("route-rules"); got != "route-rules=1&theme=dark" {
		t.Errorf("query = %q", got)
	}

	// A value from anywhere else — a hand-edited file, a stale client — must not
	// end up in a URL.
	writeThemeMode("neon")
	if got := readThemeMode(); got != "dark" {
		t.Errorf("an unknown mode was accepted: %q", got)
	}
	if err := os.WriteFile(themeFilePath(), []byte("  LIGHT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readThemeMode(); got != "light" {
		t.Errorf("a padded, upper-case mode read as %q", got)
	}
	writeThemeMode("system")
	if got := readThemeMode(); got != "light" {
		t.Errorf("system mode replaced the saved light theme: %q", got)
	}
}
