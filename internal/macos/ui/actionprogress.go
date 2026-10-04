package ui

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// Progress feedback for the menu actions that take real time.
//
// A restart (stop, resolver cleanup, sing-box re-init) and a disconnect (terminate, resolver cleanup, DNS reapply) were
// both invisible: the menu closed, the glyph kept showing the old state, and
// nothing said the click had landed — for a disconnect the ⏳ only ever appeared
// once the last VPN was down, so stopping one of several looked like nothing at
// all. While either runs the status icon pulses (spinnerAlphas) and the menu
// header names what is happening.

// spinnerFrames are the glyphs progressHeader puts before the verb.
var spinnerFrames = []string{"◐", "◓", "◑", "◒"}

// spinnerInterval is how long each frame shows: one full turn takes ~0.6s,
// fast enough to read as motion without looking frantic in the menu bar.
const spinnerInterval = 150 * time.Millisecond

// restartWatchdog bounds how long one restart may keep the spinner alive if the
// subprocess never returns, so the bar always falls back to real status. It has
// to clear the CLI's own patience by a wide margin: a watchdog that fired first
// would stop the animation mid-restart — exactly the "nothing is happening"
// impression this fixes.
const restartWatchdog = 10 * time.Minute

// startWatchdog matches restartWatchdog for the same reason: a start is the
// slow half of a restart.
const startWatchdog = 10 * time.Minute

// stopWatchdog is the same safety net for a disconnect. A stop terminates
// processes (a 3s grace each) and rewrites resolvers; nothing in it waits on a
// user or a server handshake, so it needs nowhere near the restart's patience.
const stopWatchdog = 2 * time.Minute

// quitWatchdog bounds a Quit. It cannot fall back to real status the way the
// other three do — the user asked the app to go away, and a bar left spinning
// "Quitting…" forever is worse than the silent hang it replaced — so when it
// fires the process exits regardless of the subprocess still holding it. Sized
// like stopWatchdog with room for the cache drop and the LaunchAgent unload.
const quitWatchdog = 3 * time.Minute

// spinnerAlphas is the menu-bar icon's opacity per tick while an action is in
// flight: the face pulses in the action's color instead of a glyph turning.
var spinnerAlphas = []float64{1, 0.8, 0.55, 0.35, 0.55, 0.8}

// spinnerAlpha returns the icon opacity for a tick, cycling and tolerating
// negatives.
func spinnerAlpha(tick int) float64 {
	n := len(spinnerAlphas)
	return spinnerAlphas[((tick%n)+n)%n]
}

// spinnerFrame returns the frame for a tick, cycling and tolerating negatives.
func spinnerFrame(tick int) string {
	n := len(spinnerFrames)
	if n == 0 {
		return ""
	}
	return spinnerFrames[((tick%n)+n)%n]
}

// progressHeader is the menu header while an action runs: the spinner frame (so
// an open menu animates too), the verb, and what it applies to. Names may be
// empty — an action whose targets are unknown still has to read as progress.
func progressHeader(frame, verb string, names []string) string {
	if len(names) == 0 {
		return frame + " " + verb + "…"
	}
	return frame + " " + verb + ": " + strings.Join(names, ", ") + "…"
}

// opTracker records the actions of one kind currently in flight. Each begin
// returns a token the caller ends when its subprocess finishes; tracking tokens
// rather than a flag keeps overlapping actions (a per-VPN restart started while
// "All" is still running) from ending each other's spinner. Safe for concurrent
// use: the UI thread reads it while the action goroutines mutate it.
type opTracker struct {
	mu       sync.Mutex
	next     int
	inFlight map[int][]string
}

// begin registers an action on names and returns its token.
func (t *opTracker) begin(names []string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.inFlight == nil {
		t.inFlight = map[int][]string{}
	}
	t.next++
	t.inFlight[t.next] = append([]string(nil), names...)
	return t.next
}

// end retires an action. Ending an unknown token is a no-op, so a watchdog and
// a late subprocess exit can both call it.
func (t *opTracker) end(token int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.inFlight, token)
}

// settled drops the names that have reached the state the action was working
// towards, and retires an action once none of its own are left.
//
// A start used to end only when the subprocess ended, and that subprocess does
// not return until the slowest tunnel is up: a profile waiting on a browser
// login or an approval in a bot holds it for minutes. So the menu bar went on
// saying "Connecting…" with every VPN already connected and green in its own
// list — the one place a user looks to find out whether the click landed was
// the one place still lying about it.
//
// Actions registered without names are left alone: an empty list means the
// targets were never known, not that they are all done.
func (t *opTracker) settled(done map[string]struct{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for token, names := range t.inFlight {
		if len(names) == 0 {
			continue
		}
		remaining := names[:0]
		for _, n := range names {
			if _, finished := done[n]; !finished {
				remaining = append(remaining, n)
			}
		}
		if len(remaining) == 0 {
			delete(t.inFlight, token)
			continue
		}
		t.inFlight[token] = remaining
	}
}

// clear retires every action of this kind at once, for when a later action
// overrides them and their spinners would otherwise outlive the intent.
func (t *opTracker) clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.inFlight = nil
}

// busy reports whether any action of this kind is in flight.
func (t *opTracker) busy() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.inFlight) > 0
}

// names returns the union of the in-flight actions' names, deduplicated and
// sorted so the header text is stable between frames.
func (t *opTracker) names() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	seen := map[string]struct{}{}
	var out []string
	for _, names := range t.inFlight {
		for _, n := range names {
			if _, dup := seen[n]; dup {
				continue
			}
			seen[n] = struct{}{}
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}
