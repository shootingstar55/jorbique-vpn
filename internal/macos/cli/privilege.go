package cli

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"

	"jorbiquevpn/internal/macos/control"
	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/sysexec"
)

// Privileged work used to escalate one tool at a time: a subcommand running as
// the logged-in user would call `sudo -n /usr/bin/install`, `sudo -n /bin/rm`,
// `sudo -n /usr/sbin/networksetup`, and the sudoers drop-in had to grant all of
// them. Granting a general-purpose tool is granting root outright — `install`
// and `mv` overwrite any file on the machine — so the drop-in was a local
// privilege escalation for every process running as that user.
//
// Nothing escalates itself any more. A subcommand that needs root asks the
// daemon, which is the one component that has it, over the control socket; the
// daemon takes an operation from a fixed list with checked arguments and runs
// it. There is no sudoers rule left, so there is nothing to widen and nothing
// to inherit.
//
// The sudo path below survives for one case: a person at a terminal on a
// machine where the daemon is not running. That prompts for a password, which
// is the correct answer for a human and an impossible one for the menu bar —
// which is why the bar is told plainly instead.

// escalationGuard marks a process that is already the product of an escalation,
// so a sudo that somehow lands non-root fails with a message instead of forking
// forever.
const escalationGuard = "JORBIQUE_VPN_ESCALATED"

// rootCommands change system state: they start tunnels, install launchd jobs
// or write into the system-wide config directory. Everything else — status,
// versions, configuration — reads and is left to run as whoever asked.
var rootCommands = map[string]struct{}{
	"start":               {},
	"stop":                {},
	"restart":             {},
	"-restart":            {},
	"-autostart-check":    {},
	"set-singbox-config":  {},
	"refresh-profile":     {},
	"set-route-rules":     {},
	"install-autostart":   {},
	"uninstall-autostart": {},
	"update-singbox":      {},
	"clear-singbox-cache": {},
}

// needsRoot reports whether a subcommand has to run as root.
//
// --daemon is deliberately absent: launchd starts it in the system domain, and
// a copy started any other way is a mistake to report, not to escalate.
func needsRoot(cmd string) bool {
	_, ok := rootCommands[cmd]
	return ok
}

// escalateArgs builds the sudo arguments for re-running this binary. Only ever
// this binary and the arguments it was given — the caller cannot ask for
// anything else to be run as root.
func escalateArgs(exe string, args []string) []string {
	return append([]string{"-n", exe}, args...)
}

// escalateIfNeeded re-runs this binary under sudo when the subcommand requires
// root and the process does not have it, and exits with whatever that run
// returned. It returns normally when nothing needs to happen: already root, a
// read-only subcommand, or no executable to re-run.
func escalateIfNeeded(cmd string, args []string, exe string) error {
	if !needsRoot(cmd) || os.Geteuid() == 0 || exe == "" {
		return nil
	}
	if os.Getenv(escalationGuard) == "1" {
		return core.Permission(cmd + " needs root and sudo did not provide it. Run it with sudo.")
	}

	// Read once, and keep it. Whichever of the two paths below runs the command
	// needs the payload, and standard input can only be read out of the pipe
	// once: reading it for the daemon and then falling back to sudo handed sudo
	// an input already at EOF, so route rules or sing-box settings piped in went
	// missing without a word — the command ran, on nothing.
	payload := stdinFor(cmd)
	req := control.Request{Command: cmd, Args: args[1:], Stdin: payload}
	client := control.Client{SocketPath: control.SocketPath(core.DaemonRunDir())}
	code, err := client.Run(req, os.Stdout, os.Stderr)
	switch {
	case err == nil:
		os.Exit(code)
	case !errors.Is(err, control.ErrNoDaemon):
		return err
	case !isStdinTerminal():
		// The menu bar, or anything else without someone watching: a password
		// prompt would hang forever with nobody to answer it.
		return core.Permission(cmd + " needs the Jorbique VPN Extended daemon, which is not running. " +
			"Reinstall the client, or start it with: sudo launchctl kickstart -k system/one.jorbique.vpn")
	}

	sub := sysexec.Command("/usr/bin/sudo", escalateArgs(exe, args)...)
	sub.Env = append(os.Environ(), escalationGuard+"=1")
	sub.Stdin = os.Stdin
	if _, ok := stdinCommands[cmd]; ok {
		sub.Stdin = strings.NewReader(payload)
	}
	sub.Stdout = os.Stdout
	sub.Stderr = os.Stderr
	if err := sub.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.ExitCode())
		}
		return core.Permission(cmd + " needs root: " + err.Error())
	}
	os.Exit(0)
	return nil
}

// stdinCommands read a payload from standard input: route rules, sing-box
// settings. Their input is forwarded with the request, since the daemon runs
// the operation in a process of its own, and replayed to sudo when the daemon
// is not there to take it.
var stdinCommands = map[string]struct{}{
	"set-route-rules":    {},
	"set-singbox-config": {},
}

// stdinFor reads the payload of the commands that take one, and touches
// standard input for nothing else — a read on a command that expects no input
// would block until whoever started us closed it.
func stdinFor(cmd string) string {
	if _, ok := stdinCommands[cmd]; !ok {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(os.Stdin, 64*1024))
	if err != nil {
		return ""
	}
	return string(data)
}
