package cli

import (
	"sort"
	"testing"

	"jorbiquevpn/internal/macos/control"
)

// Two lists decide whether a privileged command works, and they are edited in
// different files: rootCommands says "this needs root, ask the daemon", and
// control.operations says "the daemon will run this". A command in the first
// and not the second is refused by the daemon it was just sent to.
//
// That is not a theoretical gap. -restart was in one list and not the other,
// which is the command the menu bar's Restart entry sends: it failed on every
// machine where the daemon was running — the intended state — and failed
// silently, because the bar discards the exit status and only surfaces the
// "daemon is missing" message.
func TestEveryRootCommandIsAllowedByTheDaemon(t *testing.T) {
	var missing []string
	for cmd := range rootCommands {
		if !control.AllowedOperation(cmd) {
			missing = append(missing, cmd)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("needs root but the daemon will not run it: %v\n"+
			"Add it to control.operations, or stop calling it a root command.", missing)
	}
}

// And the other way: an operation the daemon will run that nothing routes to it
// is dead weight on the privileged surface. This is the weaker direction — a
// command could legitimately be daemon-only — so it says what to check rather
// than assuming a mistake.
func TestEveryDaemonOperationIsRoutedToIt(t *testing.T) {
	var unrouted []string
	for _, cmd := range control.Operations() {
		if _, ok := rootCommands[cmd]; !ok {
			unrouted = append(unrouted, cmd)
		}
	}
	sort.Strings(unrouted)
	if len(unrouted) > 0 {
		t.Errorf("the daemon accepts these but nothing escalates to them: %v\n"+
			"Either route them through needsRoot or drop them from the privileged surface.", unrouted)
	}
}

// The command the finding was about, named outright so a future edit that drops
// it from either list fails on something that says why it matters.
func TestRestartReachesTheDaemon(t *testing.T) {
	for _, cmd := range []string{"-restart", "-autostart-check"} {
		if !needsRoot(cmd) {
			t.Errorf("%s no longer needs root; if that is deliberate, this test should go", cmd)
		}
		if !control.AllowedOperation(cmd) {
			t.Errorf("%s cannot reach the daemon: the menu entry that sends it does nothing", cmd)
		}
		if err := control.ValidateRequest(control.Request{Command: cmd}); err != nil {
			t.Errorf("the daemon rejects the request the bar sends for %s: %v", cmd, err)
		}
	}
}
