package ui

import (
	"path/filepath"
	"strings"
)

// isBarCommandLine reports whether cmdline (as `ps -o command=` prints it)
// belongs to a menu-bar instance of this client, given the base name of the
// executable we ourselves run as.
//
// Menu-bar mode is "our executable, and no subcommand": either no arguments,
// the explicit --menu-bar the LaunchAgent passes, or the -psn_* noise Finder
// adds. Anything else is a CLI invocation (`status`, `start …`) that happens to
// share the binary and must not be mistaken for the bar.
// The executable is what is left once the arguments are taken off the end, not
// the first space-separated field: the bar is started through the app bundle
// now, and "/Applications/Jorbique VPN.app/Contents/MacOS/JorbiqueVPN" has a
// space in it. Splitting on spaces would read that as a program called
// "/Applications/Jorbique", and a second instance would start.
func isBarCommandLine(cmdline, exeName string) bool {
	rest := strings.TrimSpace(cmdline)
	if rest == "" {
		return false
	}
	for {
		i := strings.LastIndexByte(rest, ' ')
		if i < 0 {
			break
		}
		last := rest[i+1:]
		if last != "--menu-bar" && !strings.HasPrefix(last, "-psn_") {
			// A subcommand, or an argument to one: this is a CLI invocation
			// that happens to share the binary, and the base name below is
			// then that word rather than the executable, so it fails.
			break
		}
		rest = strings.TrimRight(rest[:i], " ")
	}
	base := filepath.Base(rest)
	return exeName != "" && base == exeName
}
