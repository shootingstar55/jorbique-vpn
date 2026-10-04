package scripts

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The dry-run tests next door read the plan. These run it: a whole install tree
// under a temporary prefix, upgraded from one version to the next and rolled
// back again, with `installer`, `pkgutil` and `launchctl` replaced by stubs.
// A plan that reads correctly and a script that leaves the machine on the wrong
// binary are the same text, and only this tells them apart.

// fakeClient is the installed binary. It answers everything the health check
// asks and prints its own version, which is how the test knows which build is
// in place.
const fakeClient = `#!/bin/sh
version=%s
case "$1" in
  help) echo "JorbiqueVPN ${version}" ;;
  status) echo "no tunnels running" ;;
  *) ;;
esac
exit 0
`

// The stubs. `installer` writes whatever version the "package" names, so an
// upgrade really does replace the binary; `pkgutil` keeps the receipt in a file
// the way the real one keeps it in its database.
const stubInstaller = `#!/bin/sh
pkg=""
while [ $# -gt 0 ]; do
  case "$1" in
    -pkg) pkg="$2"; shift ;;
  esac
  shift
done
version="$(cat "${pkg}")"
case "${version}" in
  *fails-to-install*) echo "installer: the package could not be installed" >&2; exit 1 ;;
esac
libexec="${JORBIQUE_VPN_ADMIN_PREFIX}/usr/local/libexec/jorbique-vpn"
mkdir -p "${libexec}"
# A build that rejects its config, or whose tunnel bookkeeping is broken: it
# installs and runs, and its status fails.
badstatus=""
case "${version}" in
  *unreadable-config* | *broken-status*) badstatus="exit 1" ;;
esac
cat >"${libexec}/JorbiqueVPN" <<EOF
#!/bin/sh
version=${version}
case "\$1" in
  help) echo "JorbiqueVPN \${version}" ;;
  status) ${badstatus:-echo 'no tunnels running'} ;;
  *) ;;
esac
exit 0
EOF
chmod 755 "${libexec}/JorbiqueVPN"
printf '%s\n' "${version}" >"${JORBIQUE_VPN_ADMIN_PREFIX}/receipt"
exit 0
`

const stubPkgutil = `#!/bin/sh
case "$1" in
  --pkg-info)
    [ -f "${JORBIQUE_VPN_ADMIN_PREFIX}/receipt" ] || exit 1
    printf 'version: %s\n' "$(cat "${JORBIQUE_VPN_ADMIN_PREFIX}/receipt")"
    ;;
  --expand)
    mkdir -p "$3"
    printf '<pkg-info version="%s"/>\n' "$(cat "$2")" >"$3/PackageInfo"
    ;;
  --forget) rm -f "${JORBIQUE_VPN_ADMIN_PREFIX}/receipt" ;;
esac
exit 0
`

// launchctl: bootstrap/bootout/kickstart succeed, and `print` reports the
// daemon loaded, which is what it does on a machine where the plist is in place.
const stubLaunchctl = `#!/bin/sh
exit 0
`

type tree struct {
	t      *testing.T
	prefix string
	path   string // PATH with the stubs first
	home   string
}

// listenOnControlSocket stands in for the daemon having come up. The health
// check asks whether anything is listening, because a daemon launchd started
// and that then died is a loaded job and a client that can start no tunnel —
// and nothing else in this tree would notice. A stub shell script cannot bind a
// unix socket; the test can, so it does.
func (tr *tree) listenOnControlSocket() {
	tr.t.Helper()
	sock := filepath.Join(tr.prefix, "var/run/jorbique-vpn/control.sock")
	mkdirAll(tr.t, filepath.Dir(sock))
	ln, err := net.Listen("unix", sock)
	if err != nil {
		// Not a skip. A tree whose daemon cannot be made to listen fails every
		// health check in this file for a reason that has nothing to do with
		// the script, and skipping would take the whole suite quietly with it.
		tr.t.Fatalf("cannot bind the control socket at %s: %v", sock, err)
	}
	tr.t.Cleanup(func() { _ = ln.Close() })
}

// shortTempDir is t.TempDir with room for a socket. A unix socket path is
// capped near a hundred characters, and the per-test directory macOS hands out
// spends most of that before the tree is laid down at all — the control socket
// lives inside the tree, so the tree goes somewhere shorter.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "mvadmin")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func newTree(t *testing.T, version string) *tree {
	t.Helper()
	for _, tool := range []string{"bash", "ditto"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not available (macOS-only script)", tool)
		}
	}
	prefix := shortTempDir(t)
	tr := &tree{t: t, prefix: prefix, home: filepath.Join(prefix, "home")}

	stubs := filepath.Join(prefix, "stubs")
	mkdirAll(t, stubs, tr.libexec(), tr.cfgDir(), filepath.Join(prefix, "Library/LaunchDaemons"),
		filepath.Join(tr.home, "Library/LaunchAgents"), filepath.Join(prefix, "Library/Logs/JorbiqueVPN"))
	writeExec(t, filepath.Join(stubs, "installer"), stubInstaller)
	writeExec(t, filepath.Join(stubs, "pkgutil"), stubPkgutil)
	writeExec(t, filepath.Join(stubs, "launchctl"), stubLaunchctl)
	tr.path = stubs + ":/usr/bin:/bin:/usr/sbin:/sbin"

	// The install as it stands before the upgrade: the binary, the receipt that
	// names its version, a config the user edited, and the two plists.
	writeExec(t, filepath.Join(tr.libexec(), "JorbiqueVPN"), fmt.Sprintf(fakeClient, version))
	writeFile(t, filepath.Join(prefix, "receipt"), version+"\n")
	writeFile(t, filepath.Join(tr.cfgDir(), "vpn_cfg.json"), `{"direct_domains":["edited-by-the-user"]}`)
	writeFile(t, filepath.Join(prefix, "Library/LaunchDaemons/one.jorbique.vpn.plist"), "<plist/>")
	writeFile(t, filepath.Join(tr.home, "Library/LaunchAgents/one.jorbique.vpn.bar.plist"), "<plist/>")
	tr.listenOnControlSocket()
	return tr
}

func (tr *tree) libexec() string { return filepath.Join(tr.prefix, "usr/local/libexec/jorbique-vpn") }
func (tr *tree) cfgDir() string {
	return filepath.Join(tr.prefix, "Library/Application Support/JorbiqueVPN/cfg")
}

// pkg writes a stand-in package: its content is the version the stub installer
// will lay down.
func (tr *tree) pkg(version string) string {
	tr.t.Helper()
	p := filepath.Join(tr.prefix, "pkgs", version+".pkg")
	mkdirAll(tr.t, filepath.Dir(p))
	writeFile(tr.t, p, version)
	return p
}

func (tr *tree) run(args ...string) (string, int) {
	tr.t.Helper()
	cmd := exec.Command("bash", append([]string{"pkg/jorbique-vpn-admin.sh"}, args...)...)
	cmd.Env = []string{
		"PATH=" + tr.path,
		"HOME=" + tr.home,
		"JORBIQUE_VPN_ADMIN_PREFIX=" + tr.prefix,
	}
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		tr.t.Fatalf("running the admin script: %v", err)
	}
	return string(out), code
}

// version asks the installed client which build it is.
func (tr *tree) version() string {
	tr.t.Helper()
	out, err := exec.Command(filepath.Join(tr.libexec(), "JorbiqueVPN"), "help").Output()
	if err != nil {
		tr.t.Fatalf("the installed client does not run: %v", err)
	}
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(out)), "JorbiqueVPN"))
}

func (tr *tree) config() string {
	tr.t.Helper()
	data, err := os.ReadFile(filepath.Join(tr.cfgDir(), "vpn_cfg.json"))
	if err != nil {
		tr.t.Fatalf("the user's config is gone: %v", err)
	}
	return string(data)
}

func TestUpgradeThenRollbackBetweenTwoVersions(t *testing.T) {
	tr := newTree(t, "2.11.5")

	out, code := tr.run("upgrade", tr.pkg("2.12.0"))
	if code != 0 {
		t.Fatalf("upgrade exit %d:\n%s", code, out)
	}
	if got := tr.version(); got != "2.12.0" {
		t.Fatalf("after the upgrade the client is %s:\n%s", got, out)
	}
	if !strings.Contains(tr.config(), "edited-by-the-user") {
		t.Errorf("the upgrade lost the user's config: %s", tr.config())
	}

	out, code = tr.run("rollback")
	if code != 0 {
		t.Fatalf("rollback exit %d:\n%s", code, out)
	}
	if got := tr.version(); got != "2.11.5" {
		t.Fatalf("after the rollback the client is %s:\n%s", got, out)
	}
	if !strings.Contains(tr.config(), "edited-by-the-user") {
		t.Errorf("the rollback lost the user's config: %s", tr.config())
	}

	// And up again. The rollback forgot the package receipt along with the
	// version it named, which used to make the next upgrade refuse to run on a
	// machine that plainly had a client on it.
	out, code = tr.run("upgrade", tr.pkg("2.12.0"))
	if code != 0 {
		t.Fatalf("the upgrade after a rollback exit %d:\n%s", code, out)
	}
	if got := tr.version(); got != "2.12.0" {
		t.Fatalf("after the second upgrade the client is %s:\n%s", got, out)
	}
}

// The point of the snapshot: a build that installs cleanly and then cannot read
// its own configuration must not be what the machine is left on.
func TestUpgradeToAnUnhealthyBuildRollsItselfBack(t *testing.T) {
	tr := newTree(t, "2.11.5")

	out, code := tr.run("upgrade", tr.pkg("2.12.0-unreadable-config"))
	if code == 0 {
		t.Fatalf("a failed health check should not exit 0:\n%s", out)
	}
	if got := tr.version(); got != "2.11.5" {
		t.Fatalf("the machine was left on %s:\n%s", got, out)
	}
	if !strings.Contains(out, "rolling back") {
		t.Errorf("the reason was not logged:\n%s", out)
	}
}

// An installer that fails part-way is the same situation, minus the working
// binary to ask.
func TestUpgradeThatCannotInstallRollsItselfBack(t *testing.T) {
	tr := newTree(t, "2.11.5")

	out, code := tr.run("upgrade", tr.pkg("2.12.0-fails-to-install"))
	if code == 0 {
		t.Fatalf("a failed installer should not exit 0:\n%s", out)
	}
	if got := tr.version(); got != "2.11.5" {
		t.Fatalf("the machine was left on %s:\n%s", got, out)
	}
}

// Every step of an upgrade ends up in the log, not only on the terminal of
// whoever ran it.
func TestUpgradeIsLoggedToTheLogDirectory(t *testing.T) {
	tr := newTree(t, "2.11.5")

	if _, code := tr.run("upgrade", tr.pkg("2.12.0")); code != 0 {
		t.Fatalf("upgrade exit %d", code)
	}
	data, err := os.ReadFile(filepath.Join(tr.prefix, "Library/Logs/JorbiqueVPN/admin.log"))
	if err != nil {
		t.Fatalf("no log: %v", err)
	}
	for _, want := range []string{"upgrading 2.11.5", "installer -pkg", "rollback/libexec", "upgraded to 2.12.0"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("the log does not mention %q:\n%s", want, data)
		}
	}
}

func mkdirAll(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// A build that installs, runs and reads its config, and then cannot say which
// tunnels are up. Its tunnel bookkeeping is broken — every Connect will fail —
// and the health check used to wave it through, because nothing it asked went
// anywhere near that code.
func TestUpgradeToABuildThatCannotReportItsTunnelsRollsItselfBack(t *testing.T) {
	tr := newTree(t, "2.11.5")

	out, code := tr.run("upgrade", tr.pkg("2.12.0-broken-status"))
	if code == 0 {
		t.Fatalf("a build that cannot report its tunnels should not stand:\n%s", out)
	}
	if got := tr.version(); got != "2.11.5" {
		t.Fatalf("the machine was left on %s:\n%s", got, out)
	}
	if !strings.Contains(out, "report its tunnels") {
		t.Errorf("the reason was not named:\n%s", out)
	}
}

// launchd reporting a job loaded says the plist parsed, not that the daemon
// lives: a binary that starts and dies before it binds its control socket
// leaves a loaded job and a client that can do nothing privileged. That machine
// has no working VPN, and it must not be the one the upgrade leaves behind.
func TestUpgradeWhereTheDaemonNeverListensRollsItselfBack(t *testing.T) {
	tr := newTree(t, "2.11.5")
	// Take the socket away: this is the tree as it looks when the new daemon
	// never got as far as binding.
	if err := os.Remove(filepath.Join(tr.prefix, "var/run/jorbique-vpn/control.sock")); err != nil {
		t.Fatal(err)
	}

	out, code := tr.run("upgrade", tr.pkg("2.12.0"))
	if code == 0 {
		t.Fatalf("a daemon that never listens should not pass the health check:\n%s", out)
	}
	if got := tr.version(); got != "2.11.5" {
		t.Fatalf("the machine was left on %s:\n%s", got, out)
	}
	if !strings.Contains(out, "not listening") {
		t.Errorf("the reason was not named:\n%s", out)
	}
}

// The failure the snapshot exists for, failing in turn. A restore that cannot
// finish is the worst state this script can leave a machine in, and the two
// things that must still happen are the ones errexit used to skip: the daemon
// is started again, and the person running it is told plainly that the machine
// is between builds rather than back on the old one.
func TestARestoreThatCannotFinishSaysSoAndStillStartsTheDaemon(t *testing.T) {
	tr := newTree(t, "2.11.5")
	// `ditto` fails only while the restore is running: the snapshot has to be
	// taken with a working one, or there would be nothing to restore.
	failingDitto := "#!/bin/sh\n" +
		"# The snapshot is allowed through; putting it back is not.\n" +
		"case \"$*\" in\n" +
		"  *rollback/libexec*/usr/local/libexec/*) echo 'ditto: Read-only file system' >&2; exit 1 ;;\n" +
		"esac\n" +
		"exec /usr/bin/ditto \"$@\"\n"
	writeExec(t, filepath.Join(tr.prefix, "stubs", "ditto"), failingDitto)

	out, code := tr.run("upgrade", tr.pkg("2.12.0-unreadable-config"))
	if code == 0 {
		t.Fatalf("a restore that failed should not exit 0:\n%s", out)
	}
	if !strings.Contains(out, "between builds") {
		t.Errorf("a half-finished restore was reported as a clean one:\n%s", out)
	}
	// The daemon comes back either way. An upgrade that ends with the machine
	// booted out of its own daemon is the one outcome nobody can recover from
	// without a second machine to read the instructions on.
	kick := strings.LastIndex(out, "launchctl kickstart")
	restore := strings.Index(out, "this step failed")
	if restore < 0 {
		t.Fatalf("the failing step was not named:\n%s", out)
	}
	if kick < restore {
		t.Errorf("the daemon was not started after the failed restore:\n%s", out)
	}
}

// Two admins on the same machine, or one who ran an upgrade twice. Whichever it
// is, the second must not start snapshotting a tree the first is halfway
// through replacing: that snapshot would be of neither build.
func TestASecondAdminRunIsLockedOut(t *testing.T) {
	tr := newTree(t, "2.11.5")
	lock := filepath.Join(tr.prefix, "Library/Application Support/JorbiqueVPN/.admin.lock")
	mkdirAll(t, lock)
	// A pid that is running: this process. A lock naming a dead process is the
	// other case, covered below.
	writeFile(t, filepath.Join(lock, "pid"), fmt.Sprintf("%d\n", os.Getpid()))

	out, code := tr.run("upgrade", tr.pkg("2.12.0"))
	if code == 0 {
		t.Fatalf("the second run was not locked out:\n%s", out)
	}
	if !strings.Contains(out, "is working on this install") {
		t.Errorf("the lock did not explain itself:\n%s", out)
	}
	if got := tr.version(); got != "2.11.5" {
		t.Errorf("the locked-out run changed the install to %s", got)
	}
}

// A machine that lost power mid-upgrade comes back with a lock and no process
// behind it. Refusing to ever run again would make the tool useless exactly
// when it is needed, so a lock whose owner is gone is cleared.
func TestALockLeftByADeadProcessIsCleared(t *testing.T) {
	tr := newTree(t, "2.11.5")
	lock := filepath.Join(tr.prefix, "Library/Application Support/JorbiqueVPN/.admin.lock")
	mkdirAll(t, lock)
	// A pid that really was a process and really is gone, rather than a number
	// picked for looking dead: pid 0 is this process's group and pid 1 is
	// launchd, and both would answer a liveness check.
	gone := exec.Command("/bin/sh", "-c", "exit 0")
	if err := gone.Run(); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(lock, "pid"), fmt.Sprintf("%d\n", gone.Process.Pid))

	out, code := tr.run("upgrade", tr.pkg("2.12.0"))
	if code != 0 {
		t.Fatalf("a stale lock blocked the upgrade: exit %d:\n%s", code, out)
	}
	if got := tr.version(); got != "2.12.0" {
		t.Fatalf("the upgrade did not happen: the client is %s:\n%s", got, out)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Errorf("the lock outlived the run that took it")
	}
}

// The other half of a power loss: the machine comes back, pids are handed out
// from the start again, and the number in the lock from before the crash now
// belongs to something else that is very much alive. Going by the pid alone,
// every upgrade and every rollback on that machine is refused until someone
// finds the directory and deletes it by hand — which is the moment they are
// least likely to be in a position to.
func TestALockWhosePidWasHandedOutAgainIsCleared(t *testing.T) {
	tr := newTree(t, "2.11.5")
	lock := filepath.Join(tr.prefix, "Library/Application Support/JorbiqueVPN/.admin.lock")
	mkdirAll(t, lock)
	// A live pid — this process — recorded as having started at a time it did
	// not. That is exactly what a reused pid looks like: alive, and not the
	// process the lock was written for.
	writeFile(t, filepath.Join(lock, "pid"), fmt.Sprintf("%d\n", os.Getpid()))
	writeFile(t, filepath.Join(lock, "start"), "Thu Jan  1 00:00:00 1970\n")

	out, code := tr.run("upgrade", tr.pkg("2.12.0"))
	if code != 0 {
		t.Fatalf("a lock from a reused pid blocked the upgrade: exit %d:\n%s", code, out)
	}
	if got := tr.version(); got != "2.12.0" {
		t.Fatalf("the upgrade did not happen: the client is %s:\n%s", got, out)
	}
	// And the live pid on its own still blocks, or this fix has taken the lock
	// apart instead of sharpening it: TestASecondAdminRunIsLockedOut writes a
	// pid and no start time, the shape a lock from an older build has.
}

// The lock is released whichever way the script ends, or the next upgrade finds
// a lock naming a process that is long gone and has to reason its way past it.
func TestTheLockIsReleasedAfterAFailedUpgrade(t *testing.T) {
	tr := newTree(t, "2.11.5")

	if _, code := tr.run("upgrade", tr.pkg("2.12.0-fails-to-install")); code == 0 {
		t.Fatal("a failing installer should not exit 0")
	}
	lock := filepath.Join(tr.prefix, "Library/Application Support/JorbiqueVPN/.admin.lock")
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatalf("the lock survived a failed upgrade")
	}
	// And the next run gets through, which is the thing that actually matters.
	if _, code := tr.run("upgrade", tr.pkg("2.12.0")); code != 0 {
		t.Errorf("the next upgrade was blocked by a leftover lock")
	}
}

// `status` is what someone runs first when a machine is misbehaving, and after
// a rollback it is the only thing that can explain why the package receipt and
// the files on disk disagree. It had no test at all.
func TestStatusReportsTheInstallAndItsSnapshot(t *testing.T) {
	tr := newTree(t, "2.11.5")

	out, code := tr.run("status")
	if code != 0 {
		t.Fatalf("status exit %d:\n%s", code, out)
	}
	for _, want := range []string{"installed:   2.11.5", "rollback to: <none>", "daemon:      loaded"} {
		if !strings.Contains(out, want) {
			t.Errorf("status does not report %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "control:     "+filepath.Join(tr.prefix, "var/run/jorbique-vpn/control.sock")) {
		t.Errorf("status does not report the control socket:\n%s", out)
	}

	if _, code := tr.run("upgrade", tr.pkg("2.12.0")); code != 0 {
		t.Fatalf("upgrade exit %d", code)
	}
	out, code = tr.run("status")
	if code != 0 {
		t.Fatalf("status after an upgrade exit %d:\n%s", code, out)
	}
	for _, want := range []string{"installed:   2.12.0", "rollback to: 2.11.5"} {
		if !strings.Contains(out, want) {
			t.Errorf("status does not report %q:\n%s", want, out)
		}
	}

	// After a rollback the receipt is gone with the version it named. Saying
	// "not installed" about a machine that plainly has a client on it is what
	// sends people reinstalling over a working install.
	if _, code := tr.run("rollback"); code != 0 {
		t.Fatalf("rollback exit %d", code)
	}
	out, code = tr.run("status")
	if code != 0 {
		t.Fatalf("status after a rollback exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "restored by a rollback") {
		t.Errorf("status does not explain the missing receipt:\n%s", out)
	}
	if strings.Contains(out, "<not installed>") {
		t.Errorf("status calls a rolled-back machine uninstalled:\n%s", out)
	}
}

// A first install is the one path with no snapshot to fall back on, so the
// health check is all that stands between the user and a broken client. It has
// to run, and it has to say so when it fails.
func TestInstallOnACleanMachine(t *testing.T) {
	tr := newTree(t, "2.11.5")
	// A machine that has never had the client: no binary, no receipt.
	if err := os.RemoveAll(tr.libexec()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(tr.prefix, "receipt")); err != nil {
		t.Fatal(err)
	}

	out, code := tr.run("install", tr.pkg("2.12.0"))
	if code != 0 {
		t.Fatalf("install exit %d:\n%s", code, out)
	}
	if got := tr.version(); got != "2.12.0" {
		t.Fatalf("after the install the client is %s:\n%s", got, out)
	}
	if !strings.Contains(out, "installed 2.12.0") {
		t.Errorf("the install did not report what it installed:\n%s", out)
	}
}

func TestInstallOfAnUnhealthyBuildFailsLoudly(t *testing.T) {
	tr := newTree(t, "2.11.5")
	if err := os.RemoveAll(tr.libexec()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(tr.prefix, "receipt")); err != nil {
		t.Fatal(err)
	}

	out, code := tr.run("install", tr.pkg("2.12.0-unreadable-config"))
	if code == 0 {
		t.Fatalf("an unhealthy first install should not exit 0:\n%s", out)
	}
	if !strings.Contains(out, "nothing to roll back to") {
		t.Errorf("the message does not say there is no way back:\n%s", out)
	}
}
