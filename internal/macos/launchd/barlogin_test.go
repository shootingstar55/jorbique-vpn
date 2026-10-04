package launchd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBarAgentPlistPath(t *testing.T) {
	got := barAgentPlistPath("/Users/test", "com.example.bar")
	want := "/Users/test/Library/LaunchAgents/com.example.bar.plist"
	if got != want {
		t.Fatalf("barAgentPlistPath() = %q, want %q", got, want)
	}
}

func TestBootoutAppLaunchAgentNoPanic(t *testing.T) {
	bootoutAppLaunchAgent(0, "", "")
	bootoutAppLaunchAgent(501, "com.example.bar", "/Users/test/Library/LaunchAgents/com.example.bar.plist")
}

func TestBarPlistContentKeepAliveForSleepRecovery(t *testing.T) {
	body := barPlistContent("/opt/old/JorbiqueVPN", "/Users/test", "com.example.bar")
	if !strings.Contains(body, "<key>KeepAlive</key>\n    <true/>") {
		t.Fatal("expected unconditional KeepAlive so the bar restarts after sleep (Quit uses bootout first)")
	}
	if strings.Contains(body, "<key>SuccessfulExit</key>") {
		t.Fatal("SuccessfulExit-only KeepAlive misses sleep exits with status 0")
	}
	if !strings.Contains(body, "<key>RunAtLoad</key>") {
		t.Fatal("expected RunAtLoad in bar agent plist")
	}
}

func TestBarPlistNeedsMigration(t *testing.T) {
	current := barPlistContent("/x", "/Users/test", "com.example.bar")
	if barPlistNeedsMigration(current) {
		t.Fatal("current template should not require migration")
	}
	runAtLoadOnly := `<?xml version="1.0"?><plist><dict><key>RunAtLoad</key><true/></dict></plist>`
	if !barPlistNeedsMigration(runAtLoadOnly) {
		t.Fatal("RunAtLoad-only plist should be migrated")
	}
	successExitOnly := strings.Replace(barPlistContent("/x", "/Users/test", "com.example.bar"),
		"<key>KeepAlive</key>\n    <true/>",
		"<key>KeepAlive</key>\n    <dict><key>SuccessfulExit</key><false/></dict>", 1)
	if !barPlistNeedsMigration(successExitOnly) {
		t.Fatal("SuccessfulExit-only KeepAlive should be migrated")
	}
}

// The per-user agent an upgrade cannot reach. The package runs as root and
// rewrites nothing in another account's ~/Library/LaunchAgents, so a plist
// naming the pre-2.12 path survives the move and launchd spends every login
// trying to start a binary that is not there.
func TestAnAgentPointingAtADeletedBinaryIsRewritten(t *testing.T) {
	gone := barPlistContent("/opt/old/JorbiqueVPN", "/Users/test", "com.example.bar")
	if !barPlistStartsAMissingBinary(gone) {
		t.Error("an agent starting a binary that is not on disk was taken as current")
	}
	// The path this test can be sure exists. Anything else that resolves to the
	// same binary by another route — the /usr/local/bin symlink, the app bundle
	// — must not count as stale either, or the menu bar is booted out and
	// restarted on every launch.
	here := barPlistContent("/bin/sh", "/Users/test", "com.example.bar")
	if barPlistStartsAMissingBinary(here) {
		t.Error("an agent starting a binary that is right there was called stale")
	}
	if got := barPlistProgram(here); got != "/bin/sh" {
		t.Errorf("barPlistProgram = %q, want the first ProgramArguments string", got)
	}
	if barPlistStartsAMissingBinary("<plist><dict></dict></plist>") {
		t.Error("a plist with no ProgramArguments should be left alone, not rewritten")
	}
}

// The agent has to start the bar through the .app, not through libexec: only a
// process inside the bundle has an icon and a name for macOS to put on its
// window — in the Dock, in Mission Control, on the Stage Manager thumbnail.
func TestTheAgentStartsTheBarThroughTheAppBundle(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "JorbiqueVPN")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(dir, "Jorbique VPN.app", "Contents", "MacOS")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(bundle, "JorbiqueVPN")
	if err := os.Symlink(binary, link); err != nil {
		t.Fatal(err)
	}
	saved := appBundleExecutable
	appBundleExecutable = link
	defer func() { appBundleExecutable = saved }()

	if got := bundledExecutable(binary); got != link {
		t.Errorf("bundledExecutable(%q) = %q, want the path inside the bundle", binary, got)
	}
	// A binary the bundle does not point at is started where it is: a developer
	// tree must not be launched through whatever .app happens to be installed.
	other := filepath.Join(dir, "other")
	if err := os.WriteFile(other, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := bundledExecutable(other); got != other {
		t.Errorf("bundledExecutable(%q) = %q, want it left alone", other, got)
	}

	// And an agent installed before the bundle held that symlink is rewritten,
	// once: after the rewrite it already names the bundle and is left alone.
	old := barPlistContent(binary, "/Users/test", "com.example.bar")
	if !barPlistStartsOutsideTheBundle(old, binary) {
		t.Error("an agent starting the bar outside the .app was taken as current")
	}
	current := barPlistContent(link, "/Users/test", "com.example.bar")
	if barPlistStartsOutsideTheBundle(current, binary) {
		t.Error("an agent already starting the bar through the .app would be rewritten on every launch")
	}
	if barPlistStartsOutsideTheBundle(barPlistContent(other, "/Users/test", "com.example.bar"), other) {
		t.Error("a bar the bundle cannot start was called stale")
	}
}
