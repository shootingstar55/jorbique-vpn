// Package sysexec is the one place in the client that starts another program.
//
// Every process the client runs — launchctl, scutil, sing-box, the
// client re-execing itself — is constructed here, so the rules that keep
// process execution safe are enforced once instead of being restated as a
// comment beside each call:
//
//   - No shell. The program is executed directly and its arguments are passed
//     as a slice, so nothing in them is expanded, quoted or otherwise
//     interpreted — that part holds for every program. On top of it, naming a
//     known shell, a known interpreter, or a launcher that picks the program
//     for us (env) is refused by name: the list below is what a call site is
//     stopped from asking for, not proof that no interpreter can be reached.
//     A renamed shell would pass it. The check is worth having because the
//     program names in this client are all constants a reviewer can read; it
//     is not a sandbox, and an allow-list is not possible while the binaries
//     come from PATH and Homebrew prefixes that differ per machine.
//   - Absolute program paths. A bare name is resolved through PATH, which the
//     client does not control; the caller has to say which binary it means.
//
// A refused command is not a panic and not an extra error return: the returned
// *exec.Cmd carries the reason in its Err field, which is what Start, Run and
// Output already report when a program cannot be launched. Call sites read the
// same as before — only the package name changes.
package sysexec

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

var (
	// ErrNotAbsolute is reported for a program named without a full path.
	ErrNotAbsolute = errors.New("sysexec: program path must be absolute")
	// ErrShell is reported for a shell or a program that resolves another
	// program for us. Both turn an argument slice back into a string the
	// system parses, which is the property this package exists to remove.
	ErrShell = errors.New("sysexec: refusing to start a shell")
)

// refused is matched against the base name of the program, lowercased. The
// interpreters are here for the same reason the shells are: perl -e and
// python -c take a program as a string, which is the shape this package
// exists to keep out of the client. Nothing in the client runs any of them.
var refused = map[string]struct{}{
	"sh": {}, "bash": {}, "zsh": {}, "dash": {}, "ksh": {}, "csh": {}, "tcsh": {}, "fish": {},
	"cmd": {}, "cmd.exe": {},
	"powershell": {}, "powershell.exe": {}, "pwsh": {}, "pwsh.exe": {},
	"env":  {},
	"perl": {}, "python": {}, "python3": {}, "ruby": {}, "node": {}, "php": {},
}

// Command is exec.Command with this package's rules applied.
func Command(prog string, args ...string) *exec.Cmd {
	// The one exec.Command in the client; checked() vets prog before it runs.
	return checked(exec.Command(prog, args...), prog) // nosemgrep
}

// CommandContext is exec.CommandContext with this package's rules applied.
func CommandContext(ctx context.Context, prog string, args ...string) *exec.Cmd {
	// The one exec.CommandContext in the client; checked() vets prog before it runs.
	return checked(exec.CommandContext(ctx, prog, args...), prog) // nosemgrep
}

func checked(cmd *exec.Cmd, prog string) *exec.Cmd {
	// A LookPath failure recorded by os/exec is the more specific answer; keep it.
	if cmd.Err == nil {
		cmd.Err = check(prog)
	}
	return cmd
}

func check(prog string) error {
	if !filepath.IsAbs(prog) {
		return fmt.Errorf("%w: %q", ErrNotAbsolute, prog)
	}
	if _, bad := refused[strings.ToLower(filepath.Base(prog))]; bad {
		return fmt.Errorf("%w: %q", ErrShell, prog)
	}
	return nil
}
