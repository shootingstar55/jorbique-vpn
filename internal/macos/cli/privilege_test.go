package cli

import (
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestOnlyStateChangingCommandsAskForRoot(t *testing.T) {
	for _, cmd := range []string{
		"start", "stop", "restart", "update-singbox",
		"install-autostart", "uninstall-autostart", "set-route-rules",
		"set-singbox-config", "clear-singbox-cache",
	} {
		if !needsRoot(cmd) {
			t.Errorf("%s changes system state but does not ask for root", cmd)
		}
	}
	for _, cmd := range []string{
		"status", "singbox-versions",
		"help", "", "--menu-bar",
	} {
		if needsRoot(cmd) {
			t.Errorf("%s only reads but asks for root", cmd)
		}
	}
	// launchd starts the daemon in the system domain. One started any other way
	// is a mistake worth reporting, not one to quietly re-run under sudo.
	if needsRoot("--daemon") {
		t.Error("--daemon escalates instead of failing")
	}
}

// The sudo line names this binary and the arguments it was given, and nothing
// else. Anything that let a caller choose the program would put back the hole
// the tool list was.
func TestEscalationRunsThisBinaryAndNothingElse(t *testing.T) {
	got := escalateArgs("/usr/local/libexec/jorbique-vpn/JorbiqueVPN", []string{"start", "intra"})
	want := []string{"-n", "/usr/local/libexec/jorbique-vpn/JorbiqueVPN", "start", "intra"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("escalateArgs = %v, want %v", got, want)
	}
	if got[0] != "-n" {
		t.Error("escalation may not prompt for a password: the menu bar runs these without a TTY")
	}
}

// A process that is already the product of an escalation and still is not root
// has to say so. Re-running would fork sudo forever.
func TestAnEscalationThatDidNotWorkStopsInsteadOfLooping(t *testing.T) {
	t.Setenv(escalationGuard, "1")
	err := escalateIfNeeded("start", []string{"start", "intra"}, "/usr/local/libexec/jorbique-vpn/JorbiqueVPN")
	if err == nil {
		t.Fatal("a failed escalation reported success")
	}
	if !strings.Contains(err.Error(), "sudo") {
		t.Errorf("unhelpful error: %v", err)
	}
}

// Nothing to re-run, nothing to do — and no crash on the way.
func TestEscalationWithoutAnExecutableIsANoOp(t *testing.T) {
	if err := escalateIfNeeded("start", []string{"start"}, ""); err != nil {
		t.Errorf("no executable should be a no-op, got %v", err)
	}
	if err := escalateIfNeeded("status", []string{"status"}, "/anything"); err != nil {
		t.Errorf("a read-only command should be a no-op, got %v", err)
	}
}

// A payload comes out of the pipe once. Both routes to root need it — the
// daemon request, and the sudo behind it when there is no daemon — so it is
// read once and kept. Reading it for the request and then falling back handed
// sudo an input already at EOF: the route rules or the sing-box settings the
// caller piped in went missing, and the command ran on nothing and reported
// success.
func TestThePayloadIsReadOnceAndHasToBeKept(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	stdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = stdin }()

	const payload = "intra\nprojects\n"
	go func() {
		_, _ = io.WriteString(w, payload)
		_ = w.Close()
	}()

	if got := stdinFor("set-route-rules"); got != payload {
		t.Fatalf("stdinFor = %q, want %q", got, payload)
	}
	// The hazard itself: there is nothing left to read a second time, which is
	// why escalateIfNeeded holds on to what it read.
	if again := stdinFor("set-route-rules"); again != "" {
		t.Fatalf("the pipe still held %q; this test no longer reproduces the drain", again)
	}
	// And a command that takes no payload does not touch standard input at all:
	// a read there would block until whoever started us closed it.
	if got := stdinFor("start"); got != "" {
		t.Errorf("start read %q from standard input", got)
	}
}
