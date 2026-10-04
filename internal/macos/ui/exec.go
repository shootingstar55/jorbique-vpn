package ui

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"

	"jorbiquevpn/internal/macos/procutil"
	"jorbiquevpn/internal/sysexec"

	"jorbiquevpn/internal/macos/ansiutil"
)

// This file ports the NSTask-based process helpers from ui.m to os/exec. None of
// them ever depended on AppKit — NSTask is only a process launcher — so this is a
// straight 1:1 port with no purego involved. The macOS-specific orchestration
// (running the menu-bar's actions via `sudo -n`, or as the GUI login user via
// `launchctl asuser` when the bar itself runs as root for Keychain parity) is
// preserved exactly; only the mechanism changes.
//
// Every OS coupling that makes the original hard to test — the effective uid,
// the environment, passwd lookups, the SCDynamicStore console user, whether
// launchctl exists, and command execution itself — is injected through the
// runner struct, so argument construction and the root/non-root branching are
// exercised by unit tests with no real sudo, launchctl or root privileges.

// runResult is the outcome of a process run: its exit status and captured
// stderr. Ports the C UiRunResult struct.
type runResult struct {
	status int
	stderr string
}

// failedToStart is the status reported when a process cannot be launched at all,
// matching ui.m's `return 127` in the NSTask @catch handlers.
const failedToStart = 127

// cmdSpec describes one process invocation for the injectable executor.
type cmdSpec struct {
	name          string
	args          []string
	env           []string // nil inherits the parent environment
	stdin         string   // written (with a trailing newline) when hasStdin
	hasStdin      bool
	captureStdout bool
	captureStderr bool
}

// cmdResult is what the executor returns for a cmdSpec.
type cmdResult struct {
	stdout string
	stderr string
	status int
}

// runner holds the orchestration's environment couplings. The zero value is not
// usable; construct with newRunner (production) or a literal with fakes (tests).
type runner struct {
	euid               int
	environ            func() []string
	lookupUser         func(string) (*user.User, error)
	launchctlPath      string
	launchctlAvailable func() bool
	// consoleOwner reads the owner of /dev/console. Injectable because the
	// tests describe a machine other than the one running them.
	consoleOwner func() string
	// consoleUserFromStore resolves the GUI login user via SCDynamicStore. It is
	// nil until Phase 5 wires the native SystemConfiguration binding; nil means
	// "unknown", and resolution falls through to /dev/console and SUDO_USER.
	consoleUserFromStore func() string
	run                  func(cmdSpec) cmdResult
}

// newRunner returns a runner wired to the real OS.
func newRunner() *runner {
	r := &runner{
		euid:                 os.Geteuid(),
		environ:              os.Environ,
		lookupUser:           user.Lookup,
		launchctlPath:        "/usr/bin/launchctl",
		consoleUserFromStore: nil, // Phase 5
		run:                  realRun,
	}
	r.launchctlAvailable = func() bool { return isExecutable(r.launchctlPath) }
	return r
}

func (r *runner) isRoot() bool { return r.euid == 0 }

// defaultConsoleOwner is the real lookup: whoever owns the console device is
// logged in at the GUI.
func defaultConsoleOwner() string {
	_, name, ok := procutil.ConsoleUserOf("/dev/console")
	if !ok {
		return ""
	}
	return name
}

// ---------------------------------------------------------------------------
// Argument builders (pure — the heart of what the tests assert).
// ---------------------------------------------------------------------------

// The bar runs the client binary directly, as the logged-in user, and never
// wraps it in sudo. A subcommand that needs root asks the daemon for it over
// the control socket (cli.escalate) — the bar itself has no privilege to pass
// on, and there is no sudoers rule left for it to use.

// sudoUserArgs builds `-n -H -u <user> <exe> <args...>` for `sudo`.
func sudoUserArgs(sudoUser, exe string, args []string) []string {
	return append([]string{"-n", "-H", "-u", sudoUser, exe}, args...)
}

// launchctlAsuserArgs builds `asuser <uid> [--] <exe> <args...>`. The `--`
// separator is used on the first pass and dropped on the second, mirroring
// ui.m's two-pass compatibility shim for older launchctl builds.
func launchctlAsuserArgs(uid int, exe string, args []string, withDashDash bool) []string {
	out := []string{"asuser", strconv.Itoa(uid)}
	if withDashDash {
		out = append(out, "--")
	}
	out = append(out, exe)
	return append(out, args...)
}

// ---------------------------------------------------------------------------
// Background launches (fire and forget).
// ---------------------------------------------------------------------------

// runNoSudoAndWait runs `<exe> <args>`, discards output, and returns the exit
// status. Ports uiRunNoSudoAndWait.
func (r *runner) runNoSudoAndWait(exe string, args []string) int {
	return r.run(cmdSpec{name: exe, args: args}).status
}

// runAsUserAndWait runs `sudo -n -H -u <user> <exe> <args>` with merged env and
// returns the exit status. Ports uiRunAsUserAndWait.
func (r *runner) runAsUserAndWait(sudoUser, exe string, args []string) int {
	return r.run(cmdSpec{
		name: "/usr/bin/sudo",
		args: sudoUserArgs(sudoUser, exe, args),
		env:  r.mergedEnvForUnixUser(sudoUser),
	}).status
}

// ---------------------------------------------------------------------------
// Run with stdin, capturing stderr.
// ---------------------------------------------------------------------------

// runWithStdin runs `<exe> <args>`, feeds stdinText (plus a newline), and
// returns status + captured stderr. Ports uiRunWithStdin.
func (r *runner) runWithStdin(exe string, args []string, stdinText string) runResult {
	return r.captureStderrWithStdin(cmdSpec{name: exe, args: args}, stdinText)
}

// runWithStdinNoSudo is runWithStdin without sudo (the GUI user's own Keychain
// chain). Ports uiRunWithStdinNoSudo.
func (r *runner) captureStderrWithStdin(spec cmdSpec, stdinText string) runResult {
	spec.stdin = stdinText
	spec.hasStdin = true
	spec.captureStderr = true
	res := r.run(spec)
	return runResult{status: res.status, stderr: res.stderr}
}

// ---------------------------------------------------------------------------
// Run capturing stdout.
// ---------------------------------------------------------------------------

// runAndCaptureOut runs `<exe> <args>`, returns stdout and exit status. Ports
// uiRunAndCaptureOut.
func (r *runner) runAndCaptureOut(exe string, args []string) (string, int) {
	res := r.run(cmdSpec{name: exe, args: args, captureStdout: true})
	return res.stdout, res.status
}

// runAndCaptureOutErr runs `<exe> <args>` and returns its combined stdout and
// stderr with the exit status. Used where the caller needs the failure text:
// the CLI prints errors to stderr, and a failing `sudo -n` explains itself
// there too.
func (r *runner) runAndCaptureOutErr(exe string, args []string) (string, int) {
	res := r.run(cmdSpec{name: exe, args: args, captureStdout: true, captureStderr: true})
	out := strings.TrimSpace(strings.TrimSpace(res.stdout) + "\n" + strings.TrimSpace(res.stderr))
	return out, res.status
}

// runSubcmdCapture runs a read-only subcommand capturing stdout, using `sudo -n`
// when the bar runs as root and the binary directly otherwise. Ports
// uiRunSubcmdCapture.
func (r *runner) runSubcmdCapture(exePath string, args []string) (string, int) {
	if exePath == "" {
		return "", failedToStart
	}
	return r.runAndCaptureOut(exePath, args)
}

// runExeStatusCapture runs `<exe> status` (via sudo -n when root). Ports
// uiRunExeStatusCapture. Running `sudo … status` as a non-root bar would falsely
// report "running as root", so the binary is used directly then.
// probeConnectivity false asks for the cheap form: which VPNs are up, without
// the HTTP probes.
func (r *runner) runExeStatusCapture(exePath string, probeConnectivity bool) (string, int) {
	args := []string{"status"}
	if !probeConnectivity {
		args = append(args, "--no-connectivity")
	}
	return r.runSubcmdCapture(exePath, args)
}

// runStreaming runs `<exe> <args>`, invokes onLine for each line the command
// writes to stdout as it arrives, and returns everything it wrote — stdout and
// stderr — with its exit status.
//
// Unlike the captured helpers it streams, so a long privileged operation can
// report progress while it runs: the first-launch sing-box download drives a
// progress bar from it, and a start tells the menu bar which tunnel just came
// up instead of leaving it to guess until the process exits. The full output is
// still returned, because the callers that used the captured helper need the
// failure text — the CLI prints its errors to stderr.
//
// It talks to os/exec directly rather than through the injectable executor
// because line streaming does not fit the buffered cmdResult model; it is
// GUI-only and not exercised by the runner unit tests.
func (r *runner) runStreaming(exe string, args []string, onLine func(string)) (string, int) {
	if exe == "" {
		return "", failedToStart
	}
	cmd := sysexec.Command(exe, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", failedToStart
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", failedToStart
	}
	var out strings.Builder
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		out.WriteString(line)
		out.WriteByte('\n')
		if onLine != nil {
			onLine(line)
		}
	}
	combined := strings.TrimSpace(strings.TrimSpace(out.String()) + "\n" + strings.TrimSpace(stderr.String()))
	if err := cmd.Wait(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return combined, ee.ExitCode()
		}
		return combined, failedToStart
	}
	return combined, 0
}

// ---------------------------------------------------------------------------
// Login-user resolution and root-to-login-user escalation.
// ---------------------------------------------------------------------------

// consoleShortUserName returns the owner of /dev/console (the GUI login user),
// or "" when it is root or undetermined. It used to run /usr/bin/stat and read
// its output; the owner of a file is a stat(2) call.
func (r *runner) consoleShortUserName() string {
	owner := r.consoleOwner
	if owner == nil {
		owner = defaultConsoleOwner
	}
	s := strings.TrimSpace(owner())
	if s == "" || s == "root" {
		return ""
	}
	return s
}

// loginUserForKeychainSubprocess resolves the GUI session's user so a root
// LaunchAgent does not touch root's login keychain. Ports
// uiLoginUserForKeychainSubprocess: SCDynamicStore, then /dev/console, then
// SUDO_USER.
func (r *runner) loginUserForKeychainSubprocess() string {
	if r.consoleUserFromStore != nil {
		if u := r.consoleUserFromStore(); u != "" {
			return u
		}
	}
	if u := r.consoleShortUserName(); u != "" {
		return u
	}
	if su := r.getenv("SUDO_USER"); su != "" && su != "root" {
		return su
	}
	return ""
}

// tryLaunchctlAsuserStdin runs the command as uid via `launchctl asuser`,
// feeding stdin and capturing stderr, retrying once without the `--` separator.
// Returns the first successful pass, or the last failure with accumulated
// stderr. Ports uiTryLaunchctlAsuserStdin.
func (r *runner) tryLaunchctlAsuserExecWait(uid int, who, exe string, args []string) int {
	if !r.launchctlAvailable() {
		return failedToStart - 1
	}
	env := r.mergedEnvForUnixUser(who)
	last := failedToStart - 1
	for pass := 0; pass < 2; pass++ {
		res := r.run(cmdSpec{
			name: r.launchctlPath,
			args: launchctlAsuserArgs(uid, exe, args, pass == 0),
			env:  env,
		})
		if res.status == 0 {
			return 0
		}
		last = res.status
	}
	return last
}

// runNoSudoAndWaitAsLoginUserIfRoot is the no-stdin counterpart. Ports
// uiRunNoSudoAndWaitAsLoginUserIfRoot.
func (r *runner) runNoSudoAndWaitAsLoginUserIfRoot(exe string, args []string) int {
	if !r.isRoot() {
		return r.runNoSudoAndWait(exe, args)
	}
	who := r.loginUserForKeychainSubprocess()
	if who == "" {
		return failedToStart - 1
	}
	if uid, ok := r.uidForUser(who); ok {
		if st := r.tryLaunchctlAsuserExecWait(uid, who, exe, args); st == 0 {
			return 0
		}
	}
	return r.runAsUserAndWait(who, exe, args)
}

// runAndCaptureOutAsLoginUserIfRoot captures stdout, running as the GUI user
// when the bar is root. Ports uiRunAndCaptureOutAsLoginUserIfRoot. Unlike the
// stdin variant, an undeterminable login user falls back to running as root
// (the original preserved this for the best-effort exit-IP lookup).
func (r *runner) runAndCaptureOutAsLoginUserIfRoot(exe string, args []string) (string, int) {
	if !r.isRoot() {
		return r.runAndCaptureOut(exe, args)
	}
	who := r.loginUserForKeychainSubprocess()
	if who == "" {
		return r.runAndCaptureOut(exe, args)
	}
	if uid, ok := r.uidForUser(who); ok && r.launchctlAvailable() {
		res := r.run(cmdSpec{
			name:          r.launchctlPath,
			args:          launchctlAsuserArgs(uid, exe, args, false),
			env:           r.mergedEnvForUnixUser(who),
			captureStdout: true,
		})
		if res.status == 0 {
			return res.stdout, 0
		}
	}
	res := r.run(cmdSpec{
		name:          "/usr/bin/sudo",
		args:          sudoUserArgs(who, exe, args),
		env:           r.mergedEnvForUnixUser(who),
		captureStdout: true,
	})
	return res.stdout, res.status
}

// ---------------------------------------------------------------------------
// Environment & user helpers.
// ---------------------------------------------------------------------------

// mergedEnvForUnixUser returns the parent environment with HOME/USER/LOGNAME/
// TMPDIR overridden for shortName and a sane default PATH. Ports
// uiMergedEnvForUnixUser. (SHELL, which os/user does not expose, is omitted: the
// subprocess is the Go CLI, which does not consult it.)
func (r *runner) mergedEnvForUnixUser(shortName string) []string {
	env := envMap(r.environ())
	if shortName != "" {
		if u, err := r.lookupUser(shortName); err == nil {
			env["HOME"] = u.HomeDir
			env["USER"] = u.Username
			env["LOGNAME"] = u.Username
			// Root LaunchAgents inherit a per-root TMPDIR under /var/folders that
			// Keychain/Cocoa choke on; force a sane temp.
			env["TMPDIR"] = "/tmp"
		}
	}
	if env["PATH"] == "" {
		env["PATH"] = "/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
	}
	return envSlice(env)
}

// uidForUser resolves shortName to a numeric uid.
func (r *runner) uidForUser(shortName string) (int, bool) {
	u, err := r.lookupUser(shortName)
	if err != nil {
		return 0, false
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, false
	}
	return uid, true
}

// getenv reads name from the runner's environment.
func (r *runner) getenv(name string) string {
	prefix := name + "="
	for _, kv := range r.environ() {
		if strings.HasPrefix(kv, prefix) {
			return kv[len(prefix):]
		}
	}
	return ""
}

// envMap turns a KEY=VALUE slice into a map.
func envMap(kv []string) map[string]string {
	m := make(map[string]string, len(kv))
	for _, e := range kv {
		if i := strings.IndexByte(e, '='); i >= 0 {
			m[e[:i]] = e[i+1:]
		}
	}
	return m
}

// envSlice turns a map back into a KEY=VALUE slice.
func envSlice(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

// isExecutable reports whether path is a regular, executable file.
func isExecutable(path string) bool {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return false
	}
	return fi.Mode()&0o111 != 0
}

// ---------------------------------------------------------------------------
// The real executor.
// ---------------------------------------------------------------------------

// realRun executes spec with os/exec, honoring its capture and stdin flags, and maps launch failures to status failedToStart (matching ui.m's
// NSTask @catch returning 127). It is the single point that touches the OS, so
// every higher-level helper above is testable by substituting runner.run.
func realRun(spec cmdSpec) cmdResult {
	// spec.name is set by the fixed helpers above (absolute system binaries).
	cmd := sysexec.Command(spec.name, spec.args...)
	if spec.env != nil {
		cmd.Env = spec.env
	}

	var stdout, stderr bytes.Buffer
	if spec.captureStdout {
		cmd.Stdout = &stdout
	}
	if spec.captureStderr {
		cmd.Stderr = &stderr
	}
	if spec.hasStdin {
		cmd.Stdin = strings.NewReader(spec.stdin + "\n")
	}

	err := cmd.Run()
	// Stripped, not raw: the CLI colours its errors for a terminal, and the
	// panels put this text on screen as it is — "\x1b[31mUUID required" and all.
	res := cmdResult{stdout: stdout.String(), stderr: ansiutil.Strip(stderr.String())}
	res.status = exitStatus(err)
	if res.status == failedToStart && spec.captureStderr && res.stderr == "" {
		res.stderr = errString(err)
	}
	return res
}

// exitStatus extracts a process exit code from exec.Run's error: 0 on success,
// the real code on a clean non-zero exit, and failedToStart when the process
// never ran.
func exitStatus(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if code := ee.ExitCode(); code >= 0 {
			return code
		}
	}
	return failedToStart
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
