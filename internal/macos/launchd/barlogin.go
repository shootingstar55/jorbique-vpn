package launchd

import (
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"jorbiquevpn/internal/macos/ansiutil"
	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/macos/fsatomic"
	"jorbiquevpn/internal/macos/procutil"
	"jorbiquevpn/internal/sysexec"
)

const launchctlExecutable = "/bin/launchctl"

const DefaultBarLabel = "one.jorbique.vpn.bar"

// appBundleExecutable is the installed binary reached through the .app: the
// bundle's Contents/MacOS holds a symlink of that name pointing at the
// executable under libexec. A process whose executable path is inside a bundle
// gets that bundle as its main bundle, and that identity is what macOS draws
// the window's icon from — in the Dock, in Mission Control, on the Stage
// Manager thumbnail. Started straight from libexec the bar has no bundle at
// all, so those places show nothing. Go resolves the symlink away
// (core.resolveExecutable), so which of the two paths started it changes
// nothing about where the client reads and writes.
//
// A variable so a test can point it at a bundle it built itself.
var appBundleExecutable = "/Applications/Jorbique VPN Extended.app/Contents/MacOS/JorbiqueVPN"

// BundledExecutable is bundledExecutable for the callers that start the menu
// bar themselves — the root bootstrap after an install, and the detached
// re-exec off a terminal. A bar started by the raw path has no bundle, and
// nothing macOS shows about a window (Dock, Mission Control, Stage Manager)
// has an icon to draw.
func BundledExecutable(executablePath string) string { return bundledExecutable(executablePath) }

// bundledExecutable returns the path the LaunchAgent should start: the one
// inside the .app when it leads to the same binary as executablePath, and
// executablePath itself otherwise — a developer tree, or an install whose
// bundle is missing the symlink.
func bundledExecutable(executablePath string) string {
	want, err := filepath.EvalSymlinks(executablePath)
	if err != nil {
		return executablePath
	}
	got, err := filepath.EvalSymlinks(appBundleExecutable)
	if err != nil || got != want {
		return executablePath
	}
	return appBundleExecutable
}

// barPlistStartsOutsideTheBundle reports whether an installed agent starts the
// bar by a path outside the .app while the bundle can start the same binary.
// Agents written before the bundle held that symlink name libexec directly, and
// the menu bar they start has no bundle: no icon on its window anywhere macOS
// shows one.
func barPlistStartsOutsideTheBundle(plistBody, executablePath string) bool {
	prog := barPlistProgram(plistBody)
	if prog == "" || prog == appBundleExecutable {
		return false
	}
	return bundledExecutable(executablePath) == appBundleExecutable
}

func barAgentPlistPath(homeDir string, label string) string {
	return filepath.Join(homeDir, "Library/LaunchAgents", label+".plist")
}

func barPlistContent(executablePath string, homeDir string, label string) string {
	// Per-user logs belong under ~/Library/Logs, under the app's own name.
	logDir := filepath.Join(homeDir, "Library/Logs/JorbiqueVPN")
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>` + label + `</string>
    <key>ProgramArguments</key>
    <array>
        <string>` + executablePath + `</string>
        <string>--menu-bar</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>LimitLoadToSessionType</key>
    <string>Aqua</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>HOME</key>
        <string>` + homeDir + `</string>
        <key>JORBIQUE_VPN_LAUNCH_AGENT</key>
        <string>1</string>
    </dict>
    <key>StandardInPath</key>
    <string>/dev/null</string>
    <key>StandardOutPath</key>
    <string>` + logDir + `/bar.out.log</string>
    <key>StandardErrorPath</key>
    <string>` + logDir + `/bar.err.log</string>
</dict>
</plist>
`
}

// barPlistNeedsMigration reports whether the on-disk LaunchAgent should be rewritten to the
// current template. Quit still calls bootout-app-launchagent before exit so KeepAlive does not
// respawn the bar; unconditional KeepAlive restarts the menu bar after sleep when macOS exits it.
func barPlistNeedsMigration(plistBody string) bool {
	if !strings.Contains(plistBody, "<key>KeepAlive</key>") {
		return true
	}
	if strings.Contains(plistBody, "<key>SuccessfulExit</key>") {
		return true
	}
	return !strings.Contains(plistBody, "<key>KeepAlive</key>\n    <true/>")
}

// barPlistProgram is the binary an installed agent starts: the first string of
// its ProgramArguments, the one launchd execs.
func barPlistProgram(plistBody string) string {
	i := strings.Index(plistBody, "<key>ProgramArguments</key>")
	if i < 0 {
		return ""
	}
	rest := plistBody[i:]
	from := strings.Index(rest, "<string>")
	to := strings.Index(rest, "</string>")
	if from < 0 || to < from {
		return ""
	}
	return strings.TrimSpace(rest[from+len("<string>") : to])
}

// barPlistStartsAMissingBinary reports whether the agent names a program that
// is no longer on disk — what a reinstall to another location leaves in every
// account's ~/Library/LaunchAgents: the package cannot rewrite a per-user plist
// for a user who is not the one running the installer, so at the next login
// launchd goes on trying to start the old path, and the menu bar never appears.
//
// The test is "the file is gone", not "the path differs from ours": the same
// binary is reachable by several paths — /usr/local/bin symlinks, the app
// bundle — and rewriting on a mismatch would boot the agent out and restart the
// menu bar on every launch that resolved its own path differently.
func barPlistStartsAMissingBinary(plistBody string) bool {
	prog := barPlistProgram(plistBody)
	if prog == "" {
		return false
	}
	_, err := os.Stat(prog)
	return os.IsNotExist(err)
}

// RefreshAppLaunchAgentIfInstalled rewrites and reloads the app LaunchAgent when the plist
// is missing or uses a legacy template (unconditional KeepAlive, or RunAtLoad-only with no
// KeepAlive — the latter fails to restart the menu bar after sleep when macOS kills the process),
// or when it starts a binary that an upgrade has since moved out from under it.
func RefreshAppLaunchAgentIfInstalled(executablePath, label string) error {
	if os.Geteuid() == 0 {
		return nil
	}
	if label == "" {
		label = DefaultBarLabel
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	plistPath := barAgentPlistPath(home, label)
	data, err := os.ReadFile(plistPath)
	if err != nil {
		return nil
	}
	if !barPlistNeedsMigration(string(data)) && !barPlistStartsAMissingBinary(string(data)) &&
		!barPlistStartsOutsideTheBundle(string(data), executablePath) {
		return nil
	}
	return InstallAppAutostart(executablePath, label)
}

func InstallAppAutostart(executablePath, label string) error {
	if os.Geteuid() == 0 {
		return core.Permission("install-app-autostart must run as the current user (not root). Use from menu: app will run as your user.")
	}
	if label == "" {
		label = DefaultBarLabel
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	plistPath := barAgentPlistPath(home, label)
	if err := writeBarPlist(executablePath, home, label); err != nil {
		return err
	}
	uid := os.Getuid()
	bootoutAppLaunchAgent(uid, label, plistPath)
	time.Sleep(200 * time.Millisecond)
	cmd := sysexec.Command(launchctlExecutable, "bootstrap", "gui/"+strconv.Itoa(uid), plistPath)
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return core.StartFailed("launchctl bootstrap failed with exit " + strconv.Itoa(ee.ExitCode()))
		}
		return core.StartFailed("launchctl bootstrap failed: " + err.Error())
	}
	println(ansiutil.Green("✓ App autostart enabled: " + plistPath))
	return nil
}

func bootoutAppLaunchAgent(uid int, label, plistPath string) {
	if uid <= 0 || label == "" {
		return
	}
	guiDomain := "gui/" + strconv.Itoa(uid)
	_ = sysexec.Command(launchctlExecutable, "bootout", guiDomain+"/"+label).Run()
	if plistPath != "" {
		_ = sysexec.Command(launchctlExecutable, "bootout", guiDomain, plistPath).Run()
	}
}

// BootoutAppLaunchAgent unloads the menu-bar LaunchAgent without removing its plist.
// Use on Quit so legacy KeepAlive jobs cannot respawn the icon seconds later.
func BootoutAppLaunchAgent(label string) error {
	if label == "" {
		label = DefaultBarLabel
	}
	uid, home, ok := resolveAppAutostartUser(label)
	if !ok {
		return nil
	}
	bootoutAppLaunchAgent(uid, label, barAgentPlistPath(home, label))
	return nil
}

// resolveAppAutostartUser returns the GUI user whose ~/Library/LaunchAgents holds the bar agent.
func resolveAppAutostartUser(label string) (uid int, home string, ok bool) {
	if label == "" {
		label = DefaultBarLabel
	}
	if os.Geteuid() != 0 {
		u, err := user.Current()
		if err != nil || u.HomeDir == "" {
			return 0, "", false
		}
		return os.Getuid(), u.HomeDir, true
	}
	if uid, home, ok := sudoAutostartUser(); ok {
		return uid, home, true
	}
	entries, _ := os.ReadDir("/Users")
	if uid, home, ok := findAppAutostartUser(entries, label, false); ok {
		return uid, home, true
	}
	// An agent can remain loaded after its plist was removed.
	return findAppAutostartUser(entries, label, true)
}

func sudoAutostartUser() (int, string, bool) {
	if su := os.Getenv("SUDO_UID"); su != "" {
		if n, err := strconv.Atoi(su); err == nil && n != 0 {
			if u, err := user.LookupId(su); err == nil {
				return n, u.HomeDir, true
			}
		}
	}
	name := os.Getenv("SUDO_USER")
	if name == "" || name == "root" {
		return 0, "", false
	}
	u, err := user.Lookup(name)
	if err != nil {
		return 0, "", false
	}
	uid, err := strconv.Atoi(u.Uid)
	return uid, u.HomeDir, err == nil
}

func findAppAutostartUser(entries []os.DirEntry, label string, loaded bool) (int, string, bool) {
	for _, e := range entries {
		name := e.Name()
		if name == "Shared" || strings.HasPrefix(name, ".") {
			continue
		}
		home := filepath.Join("/Users", name)
		if !loaded {
			if _, err := os.Stat(barAgentPlistPath(home, label)); err != nil {
				continue
			}
		}
		u, err := user.Lookup(name)
		if err != nil {
			continue
		}
		uid, err := strconv.Atoi(u.Uid)
		if err != nil || (loaded && !appAgentLoaded(uid, label)) {
			continue
		}
		return uid, home, true
	}
	return 0, "", false
}

func appAgentLoaded(uid int, label string) bool {
	status, _ := procutil.RunAndCapture(launchctlExecutable, "print", "gui/"+strconv.Itoa(uid)+"/"+label)
	return status == 0
}

// HandOffToAppLaunchAgent puts a menu bar the user opened by hand under the
// LaunchAgent, so it lives until the user quits it: macOS ends the bar's
// process around sleep, a closed lid or a fast user switch, and only the
// agent's KeepAlive brings it back. The bar used to stay up that way only when
// the "open at login" switch was on; now every launch arms the agent, and Quit
// removes it again (see UninstallAppAutostart), so a bar the user closed does
// not come back at the next login either.
//
// It writes the plist when it is missing or out of date and then loads the
// agent, or starts it when launchd already has the job. It reports whether the
// agent now owns the bar; the caller then exits instead of running a second,
// competing instance. False (as root, or when launchd refuses) means the caller
// runs the bar itself, as before.
func HandOffToAppLaunchAgent(executablePath, label string) bool {
	if os.Geteuid() == 0 {
		return false
	}
	if label == "" {
		label = DefaultBarLabel
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	plistPath := barAgentPlistPath(home, label)
	data, err := os.ReadFile(plistPath)
	if err != nil || barPlistNeedsMigration(string(data)) || barPlistStartsAMissingBinary(string(data)) ||
		barPlistStartsOutsideTheBundle(string(data), executablePath) {
		if err := writeBarPlist(executablePath, home, label); err != nil {
			return false
		}
		// A job loaded from the old file keeps the old program: reload it.
		bootoutAppLaunchAgent(os.Getuid(), label, plistPath)
	}
	domain := "gui/" + strconv.Itoa(os.Getuid())
	if st, _ := procutil.RunAndCapture(launchctlExecutable, "print", domain+"/"+label); st == 0 {
		// Loaded, and the caller found no bar running: KeepAlive has given up
		// for now (throttled) or the job was never started. Start it.
		return sysexec.Command(launchctlExecutable, "kickstart", domain+"/"+label).Run() == nil
	}
	return sysexec.Command(launchctlExecutable, "bootstrap", domain, plistPath).Run() == nil
}

// writeBarPlist writes the agent's plist without loading it.
func writeBarPlist(executablePath, home, label string) error {
	plistPath := barAgentPlistPath(home, label)
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return err
	}
	_ = os.MkdirAll(filepath.Join(home, "Library/Logs/JorbiqueVPN"), 0o755)
	// Same atomic write as the daemon plist next door: launchd bootstraps this
	// file right after, and a partial read is a login agent that does not load.
	return fsatomic.WriteFile(plistPath, []byte(barPlistContent(bundledExecutable(executablePath), home, label)), 0o644)
}

func UninstallAppAutostart(label string) error {
	if label == "" {
		label = DefaultBarLabel
	}
	_, home, ok := resolveAppAutostartUser(label)
	if !ok {
		return core.StartFailed("could not resolve user home for app autostart")
	}
	plistPath := barAgentPlistPath(home, label)
	// Do not launchctl bootout here: that terminates the menu-bar process the user is using.
	// Quit runs this first, so the bar does not return at the next login, and then
	// bootout-app-launchagent, so KeepAlive does not respawn it now.
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	println(ansiutil.Green("✓ App autostart disabled: " + plistPath))
	return nil
}

// AppAutostartStatusString reports whether the app LaunchAgent plist is installed/loaded.
func AppAutostartStatusString(label string) string {
	if label == "" {
		label = DefaultBarLabel
	}
	uid, home, ok := resolveAppAutostartUser(label)
	if !ok {
		return "App autostart: —"
	}
	plistPath := barAgentPlistPath(home, label)
	_, errStat := os.Stat(plistPath)
	plistExists := errStat == nil

	st, _ := procutil.RunAndCapture(launchctlExecutable, "print", "gui/"+strconv.Itoa(uid)+"/"+label)
	loaded := st == 0

	switch {
	case plistExists && loaded:
		return "App autostart: enabled"
	case plistExists:
		return "App autostart: installed, not loaded"
	case loaded:
		return "App autostart: loaded (stale; disable again or bootout)"
	default:
		return "App autostart: disabled"
	}
}
