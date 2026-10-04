package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The package installs a background service, so a launch with that service in
// place needs no privilege at all, and asking for one costs the user a password
// prompt and several seconds of a dialog waiting on a bootout.
func runLauncher(t *testing.T, plist, label string) (string, int) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("the launcher is macOS-only (stat -f, osascript)")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	bin := filepath.Join(dir, "JorbiqueVPN")
	script := "#!/bin/sh\necho \"menu bar started: $*\" >" + marker + "\nexit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", "pkg/JorbiqueVPN-launcher.sh")
	cmd.Env = []string{
		"PATH=/usr/bin:/bin:/usr/sbin:/sbin",
		"HOME=" + dir,
		"JORBIQUE_VPN_LAUNCHER_BIN=" + bin,
		"JORBIQUE_VPN_LAUNCHER_PLIST=" + plist,
		"JORBIQUE_VPN_LAUNCHER_LABEL=" + label,
		// Stop before the administrator dialog: it waits for a person, and a
		// test run has none.
		"JORBIQUE_VPN_LAUNCHER_DRY_RUN=1",
	}
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running the launcher: %v", err)
	}
	// The bar is started detached, so give the child a moment to write its
	// marker before deciding it never ran.
	for i := 0; i < 30; i++ {
		if data, rerr := os.ReadFile(marker); rerr == nil {
			return string(data), code
		}
		time.Sleep(50 * time.Millisecond)
	}
	return string(out), code
}

func TestLauncherStartsTheMenuBarDirectlyWhenTheServiceIsInstalled(t *testing.T) {
	dir := t.TempDir()
	plist := filepath.Join(dir, "one.jorbique.vpn.plist")
	if err := os.WriteFile(plist, []byte("<plist/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A job every macOS has loaded in the system domain, standing in for ours:
	// what is asserted is the decision, not which daemon it is.
	started, code := runLauncher(t, plist, "com.apple.opendirectoryd")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(started, "menu bar started") {
		t.Error("the launcher did not start the menu bar directly — it went for a password instead")
	}
	// Detached and with the argument: the process that shows the status item
	// must not be the .app's own, or it loses the bundle and shows nothing.
	if !strings.Contains(started, "--menu-bar") {
		t.Errorf("the menu bar was started without its argument: %q", started)
	}
}

// Without the service there is something to install, and that does need a
// password. The launcher must not quietly start the menu bar as the user: it
// would come up unable to do anything privileged.
func TestLauncherDoesNotStartTheMenuBarWhenTheServiceIsMissing(t *testing.T) {
	out, code := runLauncher(t, filepath.Join(t.TempDir(), "absent.plist"), "com.apple.opendirectoryd")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if strings.Contains(out, "menu bar started") {
		t.Error("the launcher skipped setup on a machine with no background service")
	}
	if !strings.Contains(out, "setup required") {
		t.Errorf("the launcher did not go for setup: %q", out)
	}
}

// The case that left a machine dead: the plist is on disk, so the old check
// said "set up", but launchd has no job — an upgrade that could not reload it.
// The menu bar would start and refuse every privileged action with nothing on
// screen to explain it.
func TestLauncherTreatsAPlistWithNoLoadedJobAsSetupNeeded(t *testing.T) {
	dir := t.TempDir()
	plist := filepath.Join(dir, "one.jorbique.vpn.plist")
	if err := os.WriteFile(plist, []byte("<plist/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := runLauncher(t, plist, "one.jorbique.no.such.job.for.tests")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if strings.Contains(out, "menu bar started") {
		t.Error("started the menu bar with no daemon behind it")
	}
	if !strings.Contains(out, "setup required") {
		t.Errorf("did not offer to put the service back: %q", out)
	}
}
