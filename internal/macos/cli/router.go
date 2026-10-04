package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"jorbiquevpn/internal/constants"
	"jorbiquevpn/internal/macos/ansiutil"
	"jorbiquevpn/internal/macos/boot"
	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/macos/ipgeo"
	"jorbiquevpn/internal/macos/launchd"
	"jorbiquevpn/internal/macos/procutil"
	"jorbiquevpn/internal/macos/vpnmgr"
	"jorbiquevpn/internal/sysexec"
)

const (
	sudoExecutable = "/usr/bin/sudo"
	menuBarFlag    = "--menu-bar"
)

// menuBarLockFD holds the singleton lock for menu-bar mode.
var menuBarLockFD int = -1

func acquireMenuBarSingletonLock() bool {
	if menuBarLockFD >= 0 {
		return true
	}
	const lockPath = "/tmp/jorbique-vpn-menubar.lock"
	fd, err := syscall.Open(lockPath, syscall.O_CREAT|syscall.O_RDWR, 0o644)
	if err != nil {
		return true
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		syscall.Close(fd)
		return false
	}
	menuBarLockFD = fd
	return true
}

// MenuBarRunner is invoked in menu-bar mode. The CLI router calls this to start UI.
// It is wired by main.go via SetMenuBarRunner.
var menuBarRunner func()

// SetMenuBarRunner allows main to inject the UI runner without an import cycle.
func SetMenuBarRunner(fn func()) { menuBarRunner = fn }

// stripFinderLaunchArgs removes LaunchServices noise (e.g. -psn_0_12345) that Finder passes to .app executables.
// Without this, the first "command" is not empty and menu-bar / default GUI mode never runs.
func stripFinderLaunchArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if strings.HasPrefix(a, "-psn_") {
			continue
		}
		out = append(out, a)
	}
	return out
}

// bootstrapRootInstallSudoThenMenuBar runs as root after `sudo jorbique-vpn` (no subcommand): installs the privileged daemon, then starts the real menu-bar process as the invoking user (SUDO_USER). AppKit must not stay in the root session.
// The child is started in the console GUI audit session (launchctl asuser + sudo -n -u) when possible so NSStatusItem appears after .pkg / Finder launch; otherwise falls back to plain `sudo -u`.
// When JORBIQUE_VPN_SKIP_USER_MENU_BAR=1 (optional), only install the daemon and exit; a wrapper may then exec the binary as the user.
func bootstrapRootInstallSudoThenMenuBar() error {
	sudoUser := strings.TrimSpace(os.Getenv("SUDO_USER"))
	if sudoUser == "" || sudoUser == "root" {
		return core.Permission("Use: sudo /path/to/jorbique-vpn (from your macOS account so SUDO_USER is set). Don’t become root with su -.")
	}
	mgr, err := vpnmgr.New()
	if err != nil {
		return err
	}
	// This is the one moment the client is root with a person watching: put the
	// privileged daemon in place. Everything the menu bar cannot do itself goes
	// through it afterwards, and the bar never runs as root again.
	//
	// Reinstalling one that is already loaded costs a bootout and a bootstrap —
	// seconds of a password dialog sitting on screen doing nothing — so it is
	// only done when there is nothing running.
	if mgr.DaemonLoaded() {
		fmt.Fprintln(os.Stderr, ansiutil.Green(constants.AppDisplayName+": privileged daemon already running."))
	} else if err := mgr.EnsureDaemonInstalled(); err != nil {
		fmt.Fprintln(os.Stderr, ansiutil.Red(constants.AppDisplayName+": the privileged daemon was not installed: "+err.Error()))
	} else {
		fmt.Fprintln(os.Stderr, ansiutil.Green(constants.AppDisplayName+": privileged daemon installed."))
	}
	paths := core.NewPaths()
	if strings.TrimSpace(os.Getenv("JORBIQUE_VPN_SKIP_USER_MENU_BAR")) == "1" {
		return nil
	}
	exe := launchd.BundledExecutable(paths.ExecutablePath())
	if uid, consoleName, ok := procutil.GetConsoleUser(); ok && consoleName == sudoUser {
		try := sysexec.Command("/bin/launchctl", "asuser", strconv.Itoa(uid), sudoExecutable, "-n", "-u", sudoUser, exe, menuBarFlag)
		try.Stdin = nil
		try.Stdout = nil
		try.Stderr = nil
		try.Env = os.Environ()
		if err := try.Start(); err == nil {
			fmt.Fprintln(os.Stderr, ansiutil.Green(constants.AppDisplayName+": starting the menu bar as "+sudoUser+"."))
			return nil
		}
	}
	fallback := sysexec.Command(sudoExecutable, "-u", sudoUser, exe, menuBarFlag)
	fallback.Stdin = nil
	fallback.Stdout = nil
	fallback.Stderr = nil
	fallback.Env = os.Environ()
	if err := fallback.Start(); err != nil {
		return core.StartFailed("cannot start menu bar as user " + sudoUser + ": " + err.Error())
	}
	fmt.Fprintln(os.Stderr, ansiutil.Green(constants.AppDisplayName+": starting the menu bar as "+sudoUser+"."))
	return nil
}

// Run dispatches the CLI command. Returns an error when a command fails.
func Run() error {
	// Archives can strip the bundled core's executable bit.
	boot.FixSiblingSingBox(core.NewPaths().BinDir)
	args := stripFinderLaunchArgs(os.Args[1:])
	var cmd string
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "help", "-h", "--help":
		PrintHelp(os.Stdout, os.Args[0])
		return nil
	case "bootout-app-launchagent", "install-app-autostart", "install-bar-at-login", "uninstall-app-autostart", "uninstall-bar-at-login":
		return runAppAutostart(cmd)
	case "exit-ip":
		return runExitIP(args)
	case "", menuBarFlag:
		return runMenuBar()
	}
	// Construct the manager only after obtaining permission to write system state.
	args = tunnelArgs(cmd, args)
	if err := escalateIfNeeded(cmd, args, core.NewPaths().ExecutablePath()); err != nil {
		return err
	}
	mgr, err := vpnmgr.New()
	if err != nil {
		return err
	}
	return runManagerCommand(mgr, cmd, args)
}

func runAppAutostart(cmd string) error {
	p := core.NewPaths()
	label := launchd.DefaultBarLabel
	if cfg, err := core.ReadConfig(p); err == nil {
		label = cfg.EffectivePaths().LaunchAgentBarLabel
	}
	switch cmd {
	case "bootout-app-launchagent":
		return launchd.BootoutAppLaunchAgent(label)
	case "install-app-autostart", "install-bar-at-login":
		return launchd.InstallAppAutostart(p.ExecutablePath(), label)
	default:
		return launchd.UninstallAppAutostart(label)
	}
}

func runMenuBar() error {
	if os.Geteuid() == 0 {
		if err := bootstrapRootInstallSudoThenMenuBar(); err != nil {
			return err
		}
		os.Exit(0)
	}
	paths := core.NewPaths()
	exe := launchd.BundledExecutable(paths.ExecutablePath())
	// If we're attached to a TTY, relaunch detached to free the terminal.
	// Do this before taking the singleton lock: otherwise the short-lived
	// parent can race the child and make the real menu-bar process exit.
	if isStdinTerminal() {
		// Detached re-exec of our own executable with a fixed argument.
		c := sysexec.Command(exe, menuBarFlag)
		c.Stdin = nil
		c.Stdout = nil
		c.Stderr = nil
		_ = c.Start()
		os.Exit(0)
	}
	if !waitForMenuBarSingletonLock() {
		os.Exit(0)
	}

	if menuBarRunner != nil {
		menuBarRunner()
	}
	os.Exit(0)
	return nil
}

// LaunchAgent bootstrap can overlap the old instance shutting down. Give its
// singleton lock time to be released before launchd has to retry.
func waitForMenuBarSingletonLock() bool {
	if acquireMenuBarSingletonLock() {
		return true
	}
	if os.Getenv("JORBIQUE_VPN_LAUNCH_AGENT") != "1" {
		return false
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		if acquireMenuBarSingletonLock() {
			return true
		}
	}
	return false
}

// Exit IP belongs to the console account even when the CLI was started as root.
func reexecExitIPAsUser(args []string) {
	if os.Geteuid() != 0 {
		return
	}
	uid, uname, haveConsole := procutil.GetConsoleUser()
	if !haveConsole || uname == "" {
		if su := strings.TrimSpace(os.Getenv("SUDO_USER")); su != "root" {
			uname = su
		}
	}
	if uname == "" {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	sudoArgs := append([]string{"-n", "-u", uname, exe}, args...)
	runExitIPChild(sysexec.Command(sudoExecutable, sudoArgs...))
	// Only a failure to start sudo reaches this fallback. An executed child
	// always determines our exit status, including its failures.
	if haveConsole {
		alt := append([]string{"asuser", strconv.Itoa(uid), exe}, args...)
		runExitIPChild(sysexec.Command("/bin/launchctl", alt...))
	}
}

func runExitIPChild(cmd *exec.Cmd) {
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	err := cmd.Run()
	if err == nil {
		os.Exit(0)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		os.Exit(exitErr.ExitCode())
	}
}

func runExitIP(args []string) error {
	reexecExitIPAsUser(args)
	p := core.NewPaths()
	cfg, err := core.ReadConfig(p)
	if err != nil {
		return err
	}
	var ip, cc string
	if len(args) > 1 && args[1] == "wait" {
		var ok bool
		ip, cc, ok = ipgeo.WaitStable(cfg.EffectiveAPI(), ipgeo.DefaultWaitOpts())
		if !ok {
			os.Exit(1)
		}
	} else {
		ip, cc = ipgeo.Fetch(cfg.EffectiveAPI())
	}
	if ip == "" {
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(struct {
		IP string `json:"ip"`
		CC string `json:"cc"`
	}{IP: ip, CC: cc}); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

func hasArgument(args []string, values ...string) bool {
	for _, arg := range args {
		for _, value := range values {
			if strings.TrimSpace(arg) == value {
				return true
			}
		}
	}
	return false
}

func runManagerCommand(mgr *vpnmgr.Manager, cmd string, args []string) error {
	switch cmd {
	case "status":
		// --no-connectivity: report which VPNs are up without probing their
		// hosts. The menu bar uses it while polling right after an action.
		mgr.Status(!hasArgument(args[1:], "--no-connectivity"))
	case "start":
		return mgr.Start(nonBlank(args[1:]))
	case "stop":
		stopNamed(nonBlank(args[1:]), mgr.Stop)
	case "restart":
		return mgr.Restart(nonBlank(args[1:]))
	case "set-singbox-config":
		// Three: uuid, enabled outbounds and the selector node.
		return mgr.SetSingboxConfig(readNStdinLines(3))
	case "set-route-rules":
		// Three: direct domains, bypass processes, bypass paths.
		return mgr.SetRouteRules(readNStdinLines(3))
	case "-restart":
		return mgr.AutostartCheck(false, true)
	case "-autostart-check":
		mgr.AutostartStatus()
	case "--daemon":
		return mgr.DaemonLoop()
	case "install-service":
		// The installer's: put the privileged daemon in place. Not in help —
		// nobody types this.
		return mgr.EnsureDaemonInstalled()
	case "install-autostart":
		return mgr.InstallAutostart()
	case "uninstall-autostart":
		return mgr.UninstallAutostart()
	case "refresh-profile":
		return mgr.RefreshRemoteProfile()
	case "update-singbox":
		return mgr.UpdateSingboxBinary()
	case "singbox-versions":
		return mgr.PrintSingboxVersions()
	case "clear-singbox-cache":
		// Internal (menu-bar Quit): drop sing-box's persistent DNS cache at full
		// app shutdown. Not in help, like bootout-app-launchagent.
		return mgr.ClearSingboxCache()
	default:
		return core.Usage("Unknown command '" + cmd + "'. Run 'jorbique-vpn help' for usage.")
	}
	return nil
}

// tunnelArgs defaults lifecycle commands to the single sing-box service.
func tunnelArgs(cmd string, args []string) []string {
	if cmd == "start" || cmd == "stop" || cmd == "restart" {
		if len(nonBlank(args[1:])) == 0 {
			return []string{cmd, "all"}
		}
	}
	return args
}

// nonBlank trims each argument and drops the empty ones.
func nonBlank(args []string) []string {
	var out []string
	for _, a := range args {
		if t := strings.TrimSpace(a); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// stopNamed stops what the command line named, one Stop per name; a leading
// "all" is a single Stop("all") and the rest is not looked at. No name at all
// stops nothing; tunnelArgs supplies the default for bare lifecycle commands.
func stopNamed(names []string, stop func(string)) {
	if len(names) == 0 {
		return
	}
	if names[0] == "all" {
		stop("all")
		return
	}
	for _, n := range names {
		stop(n)
	}
}

func readNStdinLines(n int) []string {
	out := []string{}
	r := bufio.NewReader(os.Stdin)
	for i := 0; i < n; i++ {
		line, err := r.ReadString('\n')
		if err != nil && line == "" {
			break
		}
		out = append(out, strings.TrimRight(line, "\r\n"))
	}
	return out
}

func isStdinTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// ReportError prints an error to stderr in red with optional hint.
func ReportError(err error) {
	if err == nil {
		return
	}
	if cli, ok := err.(*core.CLIError); ok {
		fmt.Fprint(os.Stderr, ansiutil.Red(cli.Error()+"\n"))
		if hint := cli.Suggestion(); hint != "" {
			fmt.Fprint(os.Stderr, ansiutil.Yellow("Hint: "+hint+"\n"))
		}
		return
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		fmt.Fprint(os.Stderr, ansiutil.Red("Error: "+err.Error()+"\n"))
	} else {
		fmt.Fprint(os.Stderr, ansiutil.Red("Error: "+err.Error()+"\n"))
		fmt.Fprint(os.Stderr, ansiutil.Yellow("Hint: Check logs in logs/ for details.\n"))
	}
}
