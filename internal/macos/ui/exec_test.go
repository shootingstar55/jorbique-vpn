package ui

import (
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// recorder is a fake runner.run that records every cmdSpec and returns canned
// results, so the orchestration's argument construction and root-branching can
// be asserted without real sudo, launchctl or root.
type recorder struct {
	calls   []cmdSpec
	results []cmdResult // result[i] for the i-th call; falls back to def
	def     cmdResult
}

func (rec *recorder) run(spec cmdSpec) cmdResult {
	rec.calls = append(rec.calls, spec)
	i := len(rec.calls) - 1
	if i < len(rec.results) {
		return rec.results[i]
	}
	return rec.def
}

func fakeLookup(name string) (*user.User, error) {
	return &user.User{Username: name, Uid: "501", HomeDir: "/Users/" + name}, nil
}

// rootRunner builds a root runner whose run is the given recorder. The console
// owner is taken from the recorder's first result, the way the tests used to
// feed it through a stubbed /usr/bin/stat.
func rootRunner(rec *recorder, env []string, consoleUser string) *runner {
	return &runner{
		consoleOwner: func() string {
			if rec == nil {
				return ""
			}
			return strings.TrimSpace(rec.def.stdout)
		},
		euid:                 0,
		environ:              func() []string { return env },
		lookupUser:           fakeLookup,
		launchctlPath:        "/usr/bin/launchctl",
		launchctlAvailable:   func() bool { return true },
		consoleUserFromStore: func() string { return consoleUser },
		run:                  rec.run,
	}
}

// --- pure argument builders -------------------------------------------------

func TestArgBuilders(t *testing.T) {
	if got := sudoUserArgs("alice", "/bin/exe", []string{"x"}); !reflect.DeepEqual(got, []string{"-n", "-H", "-u", "alice", "/bin/exe", "x"}) {
		t.Errorf("sudoUserArgs = %v", got)
	}
	if got := launchctlAsuserArgs(501, "/bin/exe", []string{"x"}, true); !reflect.DeepEqual(got, []string{"asuser", "501", "--", "/bin/exe", "x"}) {
		t.Errorf("launchctlAsuserArgs(dashdash) = %v", got)
	}
	if got := launchctlAsuserArgs(501, "/bin/exe", nil, false); !reflect.DeepEqual(got, []string{"asuser", "501", "/bin/exe"}) {
		t.Errorf("launchctlAsuserArgs = %v", got)
	}
}

// --- env helpers ------------------------------------------------------------

func TestMergedEnvForUnixUser(t *testing.T) {
	r := &runner{
		environ:    func() []string { return []string{"PATH=/custom", "FOO=bar"} },
		lookupUser: fakeLookup,
	}
	env := envMap(r.mergedEnvForUnixUser("alice"))
	checks := map[string]string{
		"HOME": "/Users/alice", "USER": "alice", "LOGNAME": "alice",
		"TMPDIR": "/tmp", "PATH": "/custom", "FOO": "bar",
	}
	for k, want := range checks {
		if env[k] != want {
			t.Errorf("env[%q] = %q, want %q", k, env[k], want)
		}
	}
}

func TestMergedEnvDefaultsPathWhenMissing(t *testing.T) {
	r := &runner{
		environ:    func() []string { return []string{"FOO=bar"} },
		lookupUser: fakeLookup,
	}
	env := envMap(r.mergedEnvForUnixUser(""))
	if env["PATH"] != "/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin" {
		t.Errorf("PATH default = %q", env["PATH"])
	}
	// Empty short name leaves HOME unset (no user override).
	if _, ok := env["HOME"]; ok {
		t.Errorf("HOME should be unset for empty user, got %q", env["HOME"])
	}
}

func TestGetenv(t *testing.T) {
	r := &runner{environ: func() []string { return []string{"SUDO_USER=alice", "X=1"} }}
	if got := r.getenv("SUDO_USER"); got != "alice" {
		t.Errorf("getenv = %q", got)
	}
	if got := r.getenv("MISSING"); got != "" {
		t.Errorf("getenv(missing) = %q", got)
	}
}

func TestIsExecutable(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(dir, "data.txt")
	if err := os.WriteFile(plain, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !isExecutable(exe) {
		t.Error("exe should be executable")
	}
	if isExecutable(plain) {
		t.Error("plain file should not be executable")
	}
	if isExecutable(dir) {
		t.Error("dir should not be executable")
	}
	if isExecutable(filepath.Join(dir, "nope")) {
		t.Error("missing file should not be executable")
	}
}

// --- realRun via throwaway scripts ------------------------------------------

// testScript writes an executable script and returns its path. The tests name
// the script, never /bin/sh: sysexec refuses to start a shell, which is the
// whole point of routing process creation through it.
func testScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "script")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRealRunCaptureStdout(t *testing.T) {
	res := realRun(cmdSpec{name: testScript(t, "echo hello"), captureStdout: true})
	if res.status != 0 || res.stdout != "hello\n" {
		t.Errorf("got status=%d stdout=%q", res.status, res.stdout)
	}
}

func TestRealRunCaptureStderrAndStatus(t *testing.T) {
	res := realRun(cmdSpec{name: testScript(t, "echo oops >&2; exit 4"), captureStderr: true})
	if res.status != 4 {
		t.Errorf("status = %d, want 4", res.status)
	}
	if res.stderr != "oops\n" {
		t.Errorf("stderr = %q", res.stderr)
	}
}

func TestRealRunStdin(t *testing.T) {
	res := realRun(cmdSpec{name: "/bin/cat", captureStdout: true, hasStdin: true, stdin: "payload"})
	if res.stdout != "payload\n" { // helper appends a newline
		t.Errorf("stdout = %q, want %q", res.stdout, "payload\n")
	}
}

func TestRealRunFailedToStart(t *testing.T) {
	res := realRun(cmdSpec{name: "/no/such/binary", captureStderr: true})
	if res.status != failedToStart {
		t.Errorf("status = %d, want %d", res.status, failedToStart)
	}
	if res.stderr == "" {
		t.Error("expected stderr describing the launch failure")
	}
}

// --- dispatch branching -----------------------------------------------------

// The bar runs the binary, never sudo: privilege comes from the daemon, and a
// sudo here would prompt for a password in a process with no terminal.
func TestSubcommandsRunTheBinaryDirectly(t *testing.T) {
	rec := &recorder{def: cmdResult{stdout: "out", status: 0}}
	r := rootRunner(rec, nil, "")
	out, status := r.runSubcmdCapture("/bin/exe", []string{"status"})
	if out != "out" || status != 0 {
		t.Fatalf("got out=%q status=%d", out, status)
	}
	call := rec.calls[0]
	if call.name != "/bin/exe" || !reflect.DeepEqual(call.args, []string{"status"}) {
		t.Errorf("call = %+v, want the binary itself", call)
	}
}

func TestRunSubcmdCaptureNonRootDirect(t *testing.T) {
	rec := &recorder{def: cmdResult{status: 0}}
	r := rootRunner(rec, nil, "")
	r.euid = 501 // non-root
	r.runSubcmdCapture("/bin/exe", []string{"status"})
	if rec.calls[0].name != "/bin/exe" {
		t.Errorf("non-root call = %+v", rec.calls[0])
	}
}

// The Start/Stop choosers only need the running set. Asking for the cheap
// status keeps the HTTP probes out of a menu the user just opened — and out of
// the seconds right after a stop.
func TestRunExeStatusCaptureSkipsConnectivity(t *testing.T) {
	rec := &recorder{def: cmdResult{status: 0}}
	r := rootRunner(rec, nil, "")
	r.runExeStatusCapture("/bin/exe", false)
	if len(rec.calls) != 1 {
		t.Fatalf("expected one call, got %d", len(rec.calls))
	}
	// The binary is run directly: privilege comes from the daemon now, and a
	// sudo here would prompt for a password in a process with no terminal.
	if got := rec.calls[0]; got.name != "/bin/exe" ||
		!reflect.DeepEqual(got.args, []string{"status", "--no-connectivity"}) {
		t.Errorf("call = %+v", got)
	}

	rec.calls = nil
	r.runExeStatusCapture("/bin/exe", true)
	if got := rec.calls[0]; got.name != "/bin/exe" || !reflect.DeepEqual(got.args, []string{"status"}) {
		t.Errorf("call with probing = %+v", got)
	}
}

func TestRunExeStatusCaptureEmptyExe(t *testing.T) {
	rec := &recorder{}
	r := rootRunner(rec, nil, "")
	if out, status := r.runExeStatusCapture("", true); out != "" || status != failedToStart {
		t.Errorf("got out=%q status=%d", out, status)
	}
	if len(rec.calls) != 0 {
		t.Errorf("expected no exec for empty exe, got %d", len(rec.calls))
	}
}

func TestLoginUserPrecedence(t *testing.T) {
	// SCDynamicStore wins.
	rec := &recorder{}
	r := rootRunner(rec, []string{"SUDO_USER=bob"}, "alice")
	if got := r.loginUserForKeychainSubprocess(); got != "alice" {
		t.Errorf("store user = %q, want alice", got)
	}
	if len(rec.calls) != 0 {
		t.Error("should not stat /dev/console when store resolves")
	}

	// No store -> /dev/console stat.
	rec = &recorder{def: cmdResult{stdout: "carol\n", status: 0}}
	r = rootRunner(rec, []string{"SUDO_USER=bob"}, "")
	if got := r.loginUserForKeychainSubprocess(); got != "carol" {
		t.Errorf("console user = %q, want carol", got)
	}

	// No store, console returns root -> SUDO_USER.
	rec = &recorder{def: cmdResult{stdout: "root\n", status: 0}}
	r = rootRunner(rec, []string{"SUDO_USER=bob"}, "")
	if got := r.loginUserForKeychainSubprocess(); got != "bob" {
		t.Errorf("fallback user = %q, want bob", got)
	}
}

func TestNewRunnerDefaults(t *testing.T) {
	r := newRunner()
	if r.euid != os.Geteuid() {
		t.Errorf("euid = %d, want %d", r.euid, os.Geteuid())
	}
	if r.run == nil || r.environ == nil || r.lookupUser == nil || r.launchctlAvailable == nil {
		t.Error("newRunner left a hook nil")
	}
	if r.consoleUserFromStore != nil {
		t.Error("consoleUserFromStore should be nil until Phase 5 wires SCDynamicStore")
	}
	// launchctlAvailable reflects the real FS; just exercise it for coverage.
	_ = r.launchctlAvailable()
}

func TestRunAndWaitVariants(t *testing.T) {
	rec := &recorder{def: cmdResult{status: 7}}
	r := rootRunner(rec, nil, "")

	if st := r.runNoSudoAndWait("/bin/exe", []string{"a"}); st != 7 {
		t.Errorf("runNoSudoAndWait status = %d", st)
	}
	if c := rec.calls[0]; c.name != "/bin/exe" {
		t.Errorf("runNoSudoAndWait = %+v", c)
	}
	if st := r.runAsUserAndWait("alice", "/bin/exe", []string{"b"}); st != 7 {
		t.Errorf("runAsUserAndWait status = %d", st)
	}
	if c := rec.calls[1]; c.name != "/usr/bin/sudo" || c.env == nil ||
		!reflect.DeepEqual(c.args, []string{"-n", "-H", "-u", "alice", "/bin/exe", "b"}) {
		t.Errorf("runAsUserAndWait = %+v", c)
	}
}

func TestRunWithStdinFeedsTheBinary(t *testing.T) {
	rec := &recorder{def: cmdResult{status: 0, stderr: "boom"}}
	r := rootRunner(rec, nil, "")
	res := r.runWithStdin("/bin/exe", []string{"creds"}, "secret")
	if res.stderr != "boom" {
		t.Errorf("stderr = %q", res.stderr)
	}
	c := rec.calls[0]
	if c.name != "/bin/exe" || !c.hasStdin || !c.captureStderr ||
		!reflect.DeepEqual(c.args, []string{"creds"}) {
		t.Errorf("runWithStdin = %+v", c)
	}
}

func TestRunNoSudoAndWaitAsLoginUserIfRoot(t *testing.T) {
	// Non-root: direct.
	rec := &recorder{def: cmdResult{status: 0}}
	r := rootRunner(rec, nil, "")
	r.euid = 501
	if st := r.runNoSudoAndWaitAsLoginUserIfRoot("/bin/exe", []string{"x"}); st != 0 {
		t.Errorf("non-root status = %d", st)
	}
	if rec.calls[0].name != "/bin/exe" {
		t.Errorf("non-root call = %+v", rec.calls[0])
	}

	// Root with login user: launchctl asuser exec-wait succeeds.
	rec = &recorder{def: cmdResult{status: 0}}
	r = rootRunner(rec, nil, "alice")
	if st := r.runNoSudoAndWaitAsLoginUserIfRoot("/bin/exe", []string{"x"}); st != 0 {
		t.Errorf("root status = %d", st)
	}
	if rec.calls[0].name != "/usr/bin/launchctl" {
		t.Errorf("root call = %+v", rec.calls[0])
	}

	// Root, no login user determinable.
	rec = &recorder{def: cmdResult{stdout: "root\n", status: 0}}
	r = rootRunner(rec, nil, "")
	if st := r.runNoSudoAndWaitAsLoginUserIfRoot("/bin/exe", nil); st != failedToStart-1 {
		t.Errorf("no-user status = %d, want %d", st, failedToStart-1)
	}
}

func TestRunAndCaptureOutAsLoginUserIfRootFallbackToRoot(t *testing.T) {
	// Root and no login user: the capture variant falls back to running as root.
	// The console stat returns "root" so who == ""; every run returns def.
	rec := &recorder{def: cmdResult{stdout: "root\n", status: 0}}
	r := rootRunner(rec, nil, "") // no store user, SUDO_USER unset
	_, status := r.runAndCaptureOutAsLoginUserIfRoot("/bin/exe", []string{"exit-ip"})
	if status != 0 {
		t.Errorf("status = %d", status)
	}
	last := rec.calls[len(rec.calls)-1]
	if last.name != "/bin/exe" {
		t.Errorf("expected direct root run, got %+v", last)
	}
}
