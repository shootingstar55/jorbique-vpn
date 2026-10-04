//go:build darwin

package ui

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"jorbiquevpn/internal/macos/core"
)

func TestOutermostAppBundle(t *testing.T) {
	tests := []struct{ in, want string }{
		{"/Applications/Safari.app/Contents/MacOS/Safari", "/Applications/Safari.app"},
		{"/Applications/Foo.app/Contents/Frameworks/Bar.app/Contents/MacOS/Bar", "/Applications/Foo.app"}, // outermost wins
		{"/usr/bin/curl", ""},
		{"/Applications/Xcode.app", "/Applications/Xcode.app"},
	}
	for _, tc := range tests {
		if got := outermostAppBundle(tc.in); got != tc.want {
			t.Errorf("outermostAppBundle(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestGroupsFromFlatPaths(t *testing.T) {
	flat := []string{
		"/Applications/Safari.app/Contents/MacOS/Safari",
		"/System/.../com.apple.WebKit.Networking", // webkit orphan -> Safari group
		"/Applications/Chrome.app/Contents/MacOS/Chrome",
		"/usr/local/bin/tool", // non-bundle orphan -> own group
		"  ",                  // skipped
	}
	groups := groupsFromFlatPaths(flat)

	// Sorted by display name: Chrome, Safari, tool.
	if len(groups) != 3 {
		t.Fatalf("got %d groups, want 3: %+v", len(groups), groups)
	}
	names := []string{groups[0].displayName, groups[1].displayName, groups[2].displayName}
	if !reflect.DeepEqual(names, []string{"Chrome", "Safari", "tool"}) {
		t.Fatalf("group order = %v", names)
	}
	// WebKit orphan attached to Safari.
	safari := groups[1]
	foundWebKit := false
	for _, p := range safari.paths {
		if p == "/System/.../com.apple.WebKit.Networking" {
			foundWebKit = true
		}
	}
	if !foundWebKit {
		t.Errorf("WebKit orphan not attached to Safari group: %+v", safari.paths)
	}
}

func TestGroupsFromFlatPathsWebKitOwnGroupWhenNoSafari(t *testing.T) {
	groups := groupsFromFlatPaths([]string{"/x/com.apple.WebKit.Networking"})
	if len(groups) != 1 || groups[0].displayName != "WebKit networking (system)" {
		t.Errorf("expected a standalone WebKit group, got %+v", groups)
	}
}

func TestCollectMacOSExecutables(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Test.app")
	macos := filepath.Join(app, "Contents", "MacOS")
	if err := os.MkdirAll(macos, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(macos, "Test")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A non-executable resource and a non-MacOS file must be excluded.
	if err := os.WriteFile(filepath.Join(macos, "data.bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := filepath.Join(app, "Contents", "Resources")
	_ = os.MkdirAll(res, 0o755)
	_ = os.WriteFile(filepath.Join(res, "helper"), []byte("x"), 0o755)

	got := collectMacOSExecutables(app)
	if !reflect.DeepEqual(got, []string{exe}) {
		t.Errorf("collectMacOSExecutables = %v, want [%s]", got, exe)
	}
}

func TestRouteRulesStdinAndFromConfig(t *testing.T) {
	cfg := &core.AppConfig{
		DirectDomains:         []string{"example.com"},
		DirectBypassProcesses: []string{"zoom"},
		DirectBypassPaths:     []string{"/a", "/b"},
	}
	in := routeRulesFromConfig(cfg)
	if in.Direct != "example.com" || in.Processes != "zoom" {
		t.Errorf("routeRulesFromConfig = %+v", in)
	}
	want := "example.com\nzoom\n/a\t/b"
	if got := routeRulesStdin(in); got != want {
		t.Errorf("routeRulesStdin = %q, want %q", got, want)
	}
}

func TestSingboxStdin(t *testing.T) {
	if got := singboxStdin("uuid-123", []string{"tuic", "anytls"}, ""); got != "uuid-123\ntuic,anytls\n\n\n" {
		t.Errorf("singboxStdin = %q", got)
	}
	// The mode is the fourth line; the node line before it stays blank, which
	// set-singbox-config reads as "keep the stored node".
	if got := singboxStdin("uuid-123", []string{"tuic"}, "naive"); got != "uuid-123\ntuic\n\nnaive\n" {
		t.Errorf("singboxStdin with a mode = %q", got)
	}
}

func TestSingboxModeLine(t *testing.T) {
	on, off := true, false
	for _, c := range []struct {
		in   *bool
		want string
	}{{nil, ""}, {&on, "naive"}, {&off, "standard"}} {
		if got := singboxModeLine(c.in); got != c.want {
			t.Errorf("singboxModeLine(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRouteRulesStdinKeepsMultilineFieldsSeparate(t *testing.T) {
	in := routeRulesInput{
		Direct: "one.example\rtwo.example", Processes: "Firefox\r\nGoogle Chrome",
		Paths: []string{"/Applications/Foo.app/Contents/MacOS/Foo", "/usr/bin/curl"},
	}
	want := "one.example,two.example\nFirefox,Google Chrome\n/Applications/Foo.app/Contents/MacOS/Foo\t/usr/bin/curl"
	if got := routeRulesStdin(in); got != want {
		t.Fatalf("multiline values shifted CLI fields: %q, want %q", got, want)
	}
}
