package launchd

import (
	"os"
	"strings"
	"testing"
)

// launchd's own stdout/stderr used to go next to the binary. The install
// directory is for binaries, and the daemon was the last writer still dropping
// files into it — the whole reason the layout move happened.
func TestDaemonPlistLogsOutsideTheInstallDirectory(t *testing.T) {
	body := plistContent("/usr/local/libexec/jorbique-vpn/JorbiqueVPN", "com.example.daemon")
	if strings.Contains(body, "/usr/local/libexec/jorbique-vpn/logs") {
		t.Errorf("the daemon still logs next to the binary:\n%s", body)
	}
	if !strings.Contains(body, "daemon.out.log") || !strings.Contains(body, "daemon.err.log") {
		t.Errorf("the plist lost its log paths:\n%s", body)
	}
}

// The per-user agent logs under the app's own name.
func TestBarPlistLogsUnderTheAppsOwnName(t *testing.T) {
	body := barPlistContent("/usr/local/libexec/jorbique-vpn/JorbiqueVPN", "/Users/test", "com.example.bar")
	if !strings.Contains(body, "/Users/test/Library/Logs/JorbiqueVPN/bar.out.log") {
		t.Errorf("unexpected agent log path:\n%s", body)
	}
	if barPlistNeedsMigration(body) {
		t.Error("the current agent template wants migrating")
	}
}

// A machine upgraded from the pre-2.12 layout has a LaunchDaemon still naming
// the binary that used to live in /usr/local/sbin. launchd would keep starting
// it at every boot, so the daemon half of the client would stay on the version
// the upgrade replaced.
func TestAStaleDaemonPlistIsRecognised(t *testing.T) {
	current := "/usr/local/libexec/jorbique-vpn/JorbiqueVPN"
	stale := plistContent("/opt/old/JorbiqueVPN", "com.example.daemon")
	if strings.Contains(stale, "<string>"+current+"</string>") {
		t.Error("a plist for the old path looks current")
	}
	fresh := plistContent(current, "com.example.daemon")
	if !strings.Contains(fresh, "<string>"+current+"</string>") {
		t.Errorf("a plist for the current path does not name it:\n%s", fresh)
	}
}

// An upgrade booted the running daemon out and bootstrapped again immediately.
// bootout returns before the job is gone, the bootstrap that follows fails with
// EIO, and the machine is left with the plist on disk and nothing loaded — no
// daemon, so no privileged operation works until someone notices. A job that is
// already there is restarted instead, which is all it needs to pick up the new
// binary.
// An install over a loaded daemon replaces the job rather than restarting it in
// place. `kickstart -k` restarts from the definition launchd already holds, so
// a plist naming a different binary would be ignored until reboot — and a
// different binary is the only thing RefreshDaemonIfStale fires on, which made
// the one mechanism for a stale daemon unable to fix the case that triggers it.
//
// The replacement itself needs root and a real launchd; what is asserted here
// is that the plist asks to be run and that the loaded check answers for a job
// that is not there.
func TestAnInstallOverALoadedDaemonReplacesIt(t *testing.T) {
	body := plistContent("/usr/local/libexec/jorbique-vpn/JorbiqueVPN", "com.example.daemon")
	if !strings.Contains(body, "RunAtLoad") {
		t.Fatalf("the plist does not ask launchd to run it:\n%s", body)
	}
	if Loaded("com.example.nonexistent.daemon.for.tests") {
		t.Error("a job that does not exist is reported as loaded")
	}
	// waitUnloaded is what keeps the replacement off the EIO that once left an
	// upgrade with no daemon at all: it answers true only when the job is gone,
	// and for one that never existed that is immediately.
	if !waitUnloaded("com.example.nonexistent.daemon.for.tests") {
		t.Error("a job that does not exist should count as unloaded")
	}
}

// The staleness decision, which is the whole point of RefreshDaemonIfStale and
// was reachable only as root against /Library/LaunchDaemons.
func TestDaemonPlistStaleness(t *testing.T) {
	const current = "/usr/local/libexec/jorbique-vpn/JorbiqueVPN"

	if daemonPlistIsStale(plistContent(current, "com.example.daemon"), current) {
		t.Error("a plist naming this binary is treated as stale")
	}
	// The upgrade this exists for: a machine whose plist still starts the
	// pre-2.12 binary. launchd would go on starting it at every boot.
	if !daemonPlistIsStale(plistContent("/opt/old/JorbiqueVPN", "com.example.daemon"), current) {
		t.Error("a plist naming the pre-2.12 binary is treated as current")
	}
	// A path that only contains ours: an install under a different prefix, or
	// the same tree with a suffix. Matching the substring anywhere in the file
	// would call this one current and leave the machine on the other binary.
	if !daemonPlistIsStale(plistContent(current+"-beta", "com.example.daemon"), current) {
		t.Error("a plist naming a different binary with our path as a prefix looks current")
	}
	// And the trap the tags exist for: the log paths in every plist are built
	// from the executable's directory, so the raw path appears several times
	// over in a plist that starts something else entirely.
	other := plistContent("/usr/local/libexec/jorbique-vpn/OtherBinary", "com.example.daemon")
	if !strings.Contains(other, "/usr/local/libexec/jorbique-vpn") {
		t.Fatal("the fixture does not reproduce the overlap it is here to test")
	}
	if !daemonPlistIsStale(other, current) {
		t.Error("a plist that only mentions our directory in its log paths looks current")
	}
}

// A daemon is root's to rewrite. An unprivileged process asking to refresh it
// must come back having done nothing rather than fail: this runs on every menu
// bar startup, and an error there would be reported to the user as a problem
// with their client.
func TestRefreshDaemonIfStaleIsANoOpWithoutRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("this asks what happens without root; the suite is running as root")
	}
	if err := RefreshDaemonIfStale("/usr/local/libexec/jorbique-vpn/JorbiqueVPN", "com.example.daemon"); err != nil {
		t.Errorf("an unprivileged refresh reported an error: %v", err)
	}
}

// An administrator who runs `launchctl disable` has taken the daemon out of
// service on purpose. Install ends in `launchctl enable`, and the refresh calls
// Install by itself on every root start — so an upgrade would quietly put the
// job back and nothing would say it had. The reading of print-disabled is the
// part that can be tested; both wordings launchd has used are here.
func TestADisabledDaemonIsRecognised(t *testing.T) {
	const current = `com.apple.something => false
	"com.example.daemon" => true
	"com.example.other" => false
`
	const older = `"com.example.daemon" => disabled
	"com.example.other" => enabled
`
	for _, out := range []string{current, older} {
		if !jobIsDisabled(out, "com.example.daemon") {
			t.Errorf("a disabled job read as enabled:\n%s", out)
		}
		if jobIsDisabled(out, "com.example.other") {
			t.Errorf("an enabled job read as disabled:\n%s", out)
		}
	}
	// A job launchd says nothing about was never disabled.
	if jobIsDisabled(current, "com.example.absent") {
		t.Error("a job that is not in the list was taken as disabled")
	}
	if jobIsDisabled("", "com.example.daemon") {
		t.Error("empty output was taken as a disabled job")
	}
}
