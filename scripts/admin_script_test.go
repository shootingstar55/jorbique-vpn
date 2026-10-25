package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runAdmin runs the admin script in --dry-run, where every step is printed
// instead of performed. That is the only way to exercise it without a root
// shell and a machine to reinstall — and it is also what makes the plan
// reviewable before anyone runs it for real.
func runAdmin(t *testing.T, args ...string) (string, int) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	cmd := exec.Command("bash", append([]string{"pkg/jorbique-vpn-admin.sh", "--dry-run"}, args...)...)
	// Against an empty tree, not the machine running the tests: whether this
	// host happens to have a client installed, or a snapshot left by someone
	// trying an upgrade, is not something a test should depend on.
	cmd.Env = []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"HOME=" + t.TempDir(),
		"JORBIQUE_VPN_ADMIN_PREFIX=" + t.TempDir(),
	}
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running the admin script: %v", err)
	}
	return string(out), code
}

// An upgrade has to be reversible, and the order is the whole point: stop the
// tunnels, copy the current install aside, and only then let the installer
// overwrite it. A snapshot taken after the installer ran would preserve the
// build that is being replaced by — that is, nothing worth going back to.
func TestAdminUpgradePlanSnapshotsBeforeInstalling(t *testing.T) {
	pkg := filepath.Join(t.TempDir(), "Jorbique-VPN-macOS.pkg")
	if err := os.WriteFile(pkg, []byte("not a real package"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := runAdmin(t, "upgrade", pkg)
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	stop := strings.Index(out, "stop all")
	snapshot := strings.Index(out, "rollback/libexec")
	install := strings.Index(out, "installer -pkg")
	switch {
	case stop < 0 || snapshot < 0 || install < 0:
		t.Fatalf("the plan is missing a step:\n%s", out)
	case !(stop < snapshot && snapshot < install):
		t.Errorf("plan out of order (stop %d, snapshot %d, install %d):\n%s", stop, snapshot, install, out)
	}
}

func TestAdminUpgradeRefusesMissingPackage(t *testing.T) {
	out, code := runAdmin(t, "upgrade", "no-such-file.pkg")
	if code == 0 {
		t.Fatalf("a missing package should be refused:\n%s", out)
	}
	if !strings.Contains(out, "no such package") {
		t.Errorf("unhelpful error for a missing package:\n%s", out)
	}
}

// Uninstall has to take everything the installer put outside its own directory:
// leaving the launchd jobs behind is what "uninstalled" used to mean here.
func TestAdminUninstallPlanRemovesEverythingInstalled(t *testing.T) {
	out, code := runAdmin(t, "uninstall")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	for _, want := range []string{
		"/Library/LaunchDaemons/one.jorbique.vpn.plist",
		"one.jorbique.vpn.bar.plist",
		"/Applications/Jorbique VPN Extended.app",
		"/usr/local/libexec/jorbique-vpn",
		"/usr/local/bin/jorbique-vpn",
		"pkgutil --forget one.jorbique.vpn.pkg",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the uninstall plan leaves %s behind:\n%s", want, out)
		}
	}
}

// Without --purge the user's own data survives: someone removing a broken build
// to reinstall it should not lose their configuration on the way.
// --purge means the user's data too, and the menu bar's half of it lives in
// ~/Library now: a purge that leaves it behind is one that does not uninstall.
func TestPurgeAlsoTakesThePerUserFiles(t *testing.T) {
	out, code := runAdmin(t, "uninstall", "--purge")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	for _, want := range []string{
		"Library/Application Support/JorbiqueVPN",
		"Library/Logs/JorbiqueVPN",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("--purge leaves %s behind:\n%s", want, out)
		}
	}
}

func TestAdminUninstallKeepsDataWithoutPurge(t *testing.T) {
	out, _ := runAdmin(t, "uninstall")
	if strings.Contains(out, "rm -rf /Users") || strings.Contains(out, "Library/Logs/JorbiqueVPN") {
		t.Errorf("plain uninstall reached the user's data:\n%s", out)
	}
	if !strings.Contains(out, "--purge") {
		t.Errorf("the plan does not say how to drop the data too:\n%s", out)
	}

	purged, _ := runAdmin(t, "uninstall", "--purge")
	for _, want := range []string{
		"/Library/Application Support/JorbiqueVPN",
		"/Library/Logs/JorbiqueVPN",
		"/Library/Caches/JorbiqueVPN",
		"/var/run/jorbique-vpn",
	} {
		if !strings.Contains(purged, want) {
			t.Errorf("--purge left %s behind:\n%s", want, purged)
		}
	}
}

// A rollback with nothing to roll back to must say so rather than proceed and
// delete the working install.
func TestAdminRollbackWithoutSnapshotRefuses(t *testing.T) {
	out, code := runAdmin(t, "rollback")
	if code == 0 {
		t.Errorf("rollback without a snapshot should fail:\n%s", out)
	}
	if !strings.Contains(out, "no snapshot") {
		t.Errorf("unhelpful error:\n%s", out)
	}
}

func TestAdminRejectsUnknownCommand(t *testing.T) {
	out, code := runAdmin(t, "frobnicate")
	if code == 0 || !strings.Contains(out, "unknown command") {
		t.Errorf("exit %d:\n%s", code, out)
	}
}

// Every command that changes the machine needs root; asking for it up front
// beats failing halfway through an upgrade.
//
// This is the one test that runs the script for real rather than under
// --dry-run, so it must not be able to reach a step that changes anything. It
// cannot rely on the test process being unprivileged either: CI runs the job as
// root in a container, where an unguarded `uninstall` is a real uninstall. So
// the script is given an `id` that reports an ordinary user, and the root check
// has to stop it there whoever runs the suite.
func TestAdminNeedsRootForChanges(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	shim := t.TempDir()
	id := "#!/bin/sh\n" +
		"# Only `id -u` is asked before the root check. Anything else means the\n" +
		"# script got further than this test intends to let it.\n" +
		"[ \"$1\" = \"-u\" ] || exit 64\n" +
		"echo 1000\n"
	if err := os.WriteFile(filepath.Join(shim, "id"), []byte(id), 0o755); err != nil {
		t.Fatal(err)
	}
	// An upgrade checks its package before it checks who is running it, so this
	// one has to get past that to reach the root check at all.
	pkg := filepath.Join(t.TempDir(), "Jorbique-VPN-macOS.pkg")
	if err := os.WriteFile(pkg, []byte("not a real package"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"rollback"},
		{"uninstall"},
		{"upgrade", pkg},
	} {
		cmd := exec.Command("bash", append([]string{"pkg/jorbique-vpn-admin.sh"}, args...)...)
		cmd.Env = []string{"PATH=" + shim + ":/usr/local/bin:/usr/bin:/bin", "HOME=/tmp"}
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Errorf("%v: succeeded without root:\n%s", args, out)
		}
		if !strings.Contains(string(out), "sudo") {
			t.Errorf("%v: did not ask for sudo:\n%s", args, out)
		}
	}
}
