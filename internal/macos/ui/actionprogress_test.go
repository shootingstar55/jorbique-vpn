//go:build darwin

package ui

import (
	"sync"
	"testing"
)

func TestSpinnerFrameCycles(t *testing.T) {
	if len(spinnerFrames) < 2 {
		t.Fatal("a spinner needs at least two frames to read as motion")
	}
	// One full turn returns to the first frame, and consecutive frames differ —
	// a repeat would look like a frozen glyph.
	for i := 0; i < len(spinnerFrames); i++ {
		if got, want := spinnerFrame(i), spinnerFrames[i]; got != want {
			t.Errorf("spinnerFrame(%d) = %q, want %q", i, got, want)
		}
		if spinnerFrame(i) == spinnerFrame(i+1) {
			t.Errorf("frames %d and %d are identical (%q)", i, i+1, spinnerFrame(i))
		}
	}
	if spinnerFrame(len(spinnerFrames)) != spinnerFrames[0] {
		t.Error("spinner did not wrap to the first frame")
	}
	if spinnerFrame(-1) == "" {
		t.Error("a negative tick must still yield a frame")
	}
}

func TestProgressHeader(t *testing.T) {
	tests := []struct {
		name  string
		verb  string
		names []string
		want  string
	}{
		{"one restarting", "Restarting", []string{"intra"}, "◐ Restarting: intra…"},
		{"several restarting", "Restarting", []string{"intra", "projects"}, "◐ Restarting: intra, projects…"},
		{"one disconnecting", "Disconnecting", []string{"intra"}, "◐ Disconnecting: intra…"},
		{"one connecting", "Connecting", []string{"jorbique-test"}, "◐ Connecting: jorbique-test…"},
		{"several disconnecting", "Disconnecting", []string{"intra", "jorbique-test"}, "◐ Disconnecting: intra, jorbique-test…"},
		// Unknown targets still have to read as progress rather than a blank header.
		{"nameless", "Disconnecting", nil, "◐ Disconnecting…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := progressHeader("◐", tt.verb, tt.names); got != tt.want {
				t.Errorf("progressHeader = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOpTrackerLifecycle(t *testing.T) {
	var tr opTracker
	if tr.busy() {
		t.Fatal("a fresh tracker must not be busy")
	}
	if len(tr.names()) != 0 {
		t.Fatal("a fresh tracker must report no names")
	}

	token := tr.begin([]string{"intra"})
	if !tr.busy() {
		t.Fatal("tracker not busy after begin")
	}
	tr.end(token)
	if tr.busy() {
		t.Fatal("tracker still busy after end")
	}
	tr.end(token) // ending twice (watchdog plus a late subprocess exit) is a no-op
	if tr.busy() {
		t.Fatal("a repeated end made the tracker busy again")
	}
}

// Overlapping restarts must not end each other's spinner: the tracker stays
// busy until the last one finishes.
func TestOpTrackerOverlap(t *testing.T) {
	var tr opTracker
	all := tr.begin([]string{"intra", "projects"})
	one := tr.begin([]string{"intra"})

	names := tr.names()
	want := []string{"intra", "projects"}
	if len(names) != len(want) {
		t.Fatalf("names = %v, want %v (deduplicated)", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("names = %v, want %v (sorted, deduplicated)", names, want)
		}
	}

	tr.end(one)
	if !tr.busy() {
		t.Fatal("tracker stopped being busy while the other restart was still running")
	}
	tr.end(all)
	if tr.busy() {
		t.Fatal("tracker still busy after both restarts ended")
	}
}

// The UI thread reads the tracker while restart goroutines mutate it.
func TestOpTrackerConcurrent(t *testing.T) {
	var tr opTracker
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token := tr.begin([]string{"intra"})
			tr.busy()
			tr.names()
			tr.end(token)
		}()
	}
	wg.Wait()
	if tr.busy() {
		t.Fatal("tracker left busy after every restart ended")
	}
}

// The glyph animates for a disconnect on its own, and a restart — which stops
// the tunnel on its way — is what the header names when both are in flight.
func TestCurrentAction(t *testing.T) {
	c := &statusBarController{}
	if _, ok := c.currentAction(); ok {
		t.Fatal("an idle bar must not animate")
	}

	stop := c.stopping.begin([]string{"intra"})
	act, ok := c.currentAction()
	if !ok || act.verb != "Disconnecting" || len(act.names) != 1 || act.names[0] != "intra" {
		t.Fatalf("currentAction = %+v/%v, want a Disconnecting action", act, ok)
	}
	// Going away is red, like the off state it leads to.
	if act.color != "stop" {
		t.Errorf("disconnect color = %q, want the red one", act.color)
	}

	// A connect outranks a disconnect: it is the one that can sit for minutes
	// on a browser login, which is exactly when the user needs to see progress.
	start := c.starting.begin([]string{"jorbique-test"})
	act, _ = c.currentAction()
	if act.verb != "Connecting" || act.names[0] != "jorbique-test" {
		t.Errorf("currentAction = %+v, want the connect to win over the disconnect", act)
	}
	// Working towards connected is yellow.
	if act.color != "yellow" {
		t.Errorf("connect color = %q, want the yellow one", act.color)
	}

	restart := c.restart.begin([]string{"projects"})
	if act, _ := c.currentAction(); act.verb != "Restarting" || act.color != "yellow" {
		t.Errorf("currentAction = %+v, want a yellow Restarting", act)
	}

	c.restart.end(restart)
	if act, _ := c.currentAction(); act.verb != "Connecting" {
		t.Errorf("verb = %q, want the connect to keep animating", act.verb)
	}
	c.starting.end(start)
	if act, _ := c.currentAction(); act.verb != "Disconnecting" {
		t.Errorf("verb = %q, want the disconnect to keep animating", act.verb)
	}

	// Quitting outranks everything: the app is on its way out.
	c.quitting.begin(nil)
	if act, _ := c.currentAction(); act.verb != "Quitting" || act.color != "stop" {
		t.Errorf("currentAction = %+v, want a red Quitting", act)
	}
	c.quitting = opTracker{}
	c.stopping.end(stop)
	if _, ok := c.currentAction(); ok {
		t.Error("animation continued after every action ended")
	}
}

// The report: every VPN connected and green in the menu's own list, while the
// header above it still said "Connecting…". It said so because the subprocess
// had not returned — and it does not return until the slowest tunnel is up,
// which for a profile waiting on an approval in a bot is minutes.
func TestConnectingEndsWhenTheTargetsAreUp(t *testing.T) {
	var tr opTracker
	token := tr.begin([]string{"intra", "projects", "corp"})

	tr.settled(map[string]struct{}{"projects": {}, "corp": {}})
	if !tr.busy() {
		t.Fatal("the action ended while one VPN was still connecting")
	}
	if got := tr.names(); len(got) != 1 || got[0] != "intra" {
		t.Errorf("header names = %v, want only the VPN still connecting", got)
	}

	tr.settled(map[string]struct{}{"intra": {}})
	if tr.busy() {
		t.Error("still connecting with every target up")
	}
	// The subprocess exits later and ends its token; retiring it twice is fine.
	tr.end(token)
}

// An action whose targets were never known has nothing to compare against, so
// it keeps running until its subprocess says otherwise.
func TestAnActionWithNoNamesIsNotRetiredByStatus(t *testing.T) {
	var tr opTracker
	tr.begin(nil)
	tr.settled(map[string]struct{}{"intra": {}, "projects": {}})
	if !tr.busy() {
		t.Error("an action with unknown targets was retired by a status refresh")
	}
}

// A stop is the user overriding whatever the healers were doing: the restart
// that was in flight must stop claiming the header, or the bar keeps animating
// "Restarting" over a tunnel the user just took down.
func TestStopClearsRestartSpinner(t *testing.T) {
	c := &statusBarController{}
	c.restart.begin([]string{"jorbique-prod"})
	if act, _ := c.currentAction(); act.verb != "Restarting" {
		t.Fatalf("currentAction = %q, want Restarting before the stop", act.verb)
	}

	c.restart.clear()
	c.starting.clear()
	c.stopping.begin([]string{"jorbique-prod"})

	act, ok := c.currentAction()
	if !ok || act.verb != "Disconnecting" {
		t.Fatalf("currentAction = %+v/%v, want Disconnecting once the stop lands", act, ok)
	}
	if c.restart.busy() {
		t.Error("the restart tracker still animates after being cleared")
	}
}
