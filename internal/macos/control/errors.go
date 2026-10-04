package control

import "jorbiquevpn/internal/macos/core"

// errUnsupported names an operation the daemon does not offer. The message says
// what was asked rather than only that it was refused: the caller is our own
// menu bar, and a typo in a subcommand name should read as one.
func errUnsupported(cmd string) error {
	if cmd == "" {
		return core.BadConfig("control: no operation given")
	}
	return core.BadConfig("control: " + cmd + " is not an operation this daemon performs")
}

func errBadArgument(cmd, arg string) error {
	return core.BadConfig("control: " + cmd + " will not take " + quote(arg))
}

func quote(s string) string {
	if s == "" {
		return `""`
	}
	if len(s) > 64 {
		s = s[:64] + "…"
	}
	return `"` + s + `"`
}
