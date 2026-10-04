package control

import (
	"path/filepath"
	"regexp"
	"strings"
)

// The privileged work of this client — starting tunnels, setting DNS,
// installing launchd jobs — happens in one place: the daemon, running as root.
// Everything else asks it over a unix socket, and this file is the whole of
// what may be asked.
//
// The old arrangement was a sudoers drop-in: the menu bar ran `sudo -n <binary>
// <subcommand>`, so anything that could run as the logged-in user could run any
// subcommand of ours as root, and for a while any of a dozen general-purpose
// tools too. Here the caller names an operation from a fixed list and arguments
// that have to survive a check; it never names a program, a path or a file.

// SocketName is the control socket, in the daemon's run directory.
const SocketName = "control.sock"

// SocketPath returns the control socket inside a run directory.
func SocketPath(runDir string) string { return filepath.Join(runDir, SocketName) }

// Request is one operation. Stdin carries the payload of the subcommands that
// read one (route rules, sing-box settings) — those read a fixed number of
// lines and validate them, and it is never a filename.
type Request struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Stdin   string   `json:"stdin,omitempty"`
}

// Response is one line of output, or the final exit status. Exactly one of the
// two is set: a message with Exit set is the last one on the connection.
type Response struct {
	Line   string `json:"line,omitempty"`
	Stderr bool   `json:"stderr,omitempty"`
	Exit   *int   `json:"exit,omitempty"`
}

// operations are the commands the daemon will run. The list is the privileged
// surface of the whole client: a command that is not here cannot be reached
// through the socket, whatever the caller sends.
// It has to hold every command cli.rootCommands names. A command that needs
// root and is not here cannot reach it: the bar is not root and has no sudo, so
// the request is refused and the menu entry does nothing. cli's
// TestEveryRootCommandIsAllowedByTheDaemon checks the two against each other,
// because this went unnoticed for the whole of -restart.
var operations = map[string]struct{}{
	"start":               {},
	"stop":                {},
	"restart":             {},
	"-restart":            {},
	"-autostart-check":    {},
	"update-singbox":      {},
	"clear-singbox-cache": {},
	"set-route-rules":     {},
	"set-singbox-config":  {},
	"refresh-profile":     {},
	"install-autostart":   {},
	"uninstall-autostart": {},
}

// AllowedOperation reports whether the daemon will run this command.
func AllowedOperation(cmd string) bool {
	_, ok := operations[cmd]
	return ok
}

// Operations lists what the daemon will run, for the caller that has to keep
// its own list in step with this one.
func Operations() []string {
	out := make([]string, 0, len(operations))
	for cmd := range operations {
		out = append(out, cmd)
	}
	return out
}

// argument accepts a VPN name or one of the words the subcommands take. VPN
// names are letters, digits, spaces, dots, dashes and underscores — "Jorbique
// VPN". Everything that could name something on
// disk is out: no slashes, no "..", no leading dash, nothing with a null or a
// newline in it.
var argument = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,63}$`)

// ValidateRequest reports what is wrong with a request, or nil when the daemon
// may run it. It is deliberately strict: the arguments reach a root process,
// and a name that is not a name is a bug or an attack, never a request worth
// guessing at.
func ValidateRequest(r Request) error {
	cmd := strings.TrimSpace(r.Command)
	if cmd == "" {
		return errUnsupported("")
	}
	if !AllowedOperation(cmd) {
		return errUnsupported(cmd)
	}
	if len(r.Args) > 16 {
		return errBadArgument(cmd, "too many arguments")
	}
	// No operation the daemon runs reads a flag, so a flag-shaped argument is
	// refused like any other that is not a name.
	for _, a := range r.Args {
		if !argument.MatchString(a) {
			return errBadArgument(cmd, a)
		}
		if strings.Contains(a, "..") {
			return errBadArgument(cmd, a)
		}
	}
	if len(r.Stdin) > 64*1024 {
		return errBadArgument(cmd, "oversized input")
	}
	return nil
}
