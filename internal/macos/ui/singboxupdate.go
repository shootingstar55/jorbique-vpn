//go:build darwin

package ui

import (
	"crypto/sha256"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"jorbiquevpn/internal/macos/applekit"
)

// Keeping sing-box current without being a nuisance.
//
// The binary is not shipped in the installer — the client fetches it from the
// update server — and the check used to run once, at launch. That is wrong in
// both directions: a Mac left running for a week never noticed a new build,
// while someone who restarted the app ten times in an hour asked the server ten
// times. The check now runs while the app is open, on a schedule that survives
// restarts and sleep, and the download is visible instead of silent.

const (
	// singboxCheckInterval is how often the client asks the update server what
	// it serves. sing-box builds land at most daily, so six hours picks one up
	// within a working day without turning every idle client into a poller.
	singboxCheckInterval = 6 * time.Hour

	// singboxCheckJitter spreads a fleet that was installed — or woke — at the
	// same time, so the office does not hit the update server within the same
	// second of every interval. The offset is derived from the hostname, so it
	// is stable for a machine instead of re-rolled per tick: a moving offset
	// would make "is a check due" flap around the boundary.
	singboxCheckJitter = 30 * time.Minute

	// singboxRetryInterval is the wait after a failed check or download. Being
	// offline at launch must not mean no update for six hours, and must not
	// mean retrying on every tick either.
	singboxRetryInterval = time.Hour

	// singboxTickInterval is how often the loop wakes to ask whether a check is
	// due. Due-ness is computed from a stored wall-clock timestamp rather than
	// from the ticker itself, so a laptop that slept through its window checks
	// on the first wake instead of drifting by however long the lid was shut.
	singboxTickInterval = 15 * time.Minute

	// singboxCheckStateFile remembers when the last check happened and whether
	// it worked, so quitting and relaunching does not restart the schedule.
	singboxCheckStateFile = "singbox-update-check"
)

// singboxCheckDue reports whether the client should ask the update server
// again. A never-checked install (no state file) is always due — that is the
// fresh install that has no binary at all.
//
// So is an install with no sing-box on disk, whatever the state file says. The
// state lives in the user's run directory and outlives the binary it describes:
// after a reinstall that replaced the install directory, a "checked recently,
// all good" from before the move left the client with no sing-box and no
// intention of fetching one for another six hours, warning "tool not found"
// every fifteen seconds in the meantime.
func singboxCheckDue(last time.Time, lastFailed bool, jitter time.Duration, now time.Time) bool {
	if last.IsZero() {
		return true
	}
	wait := singboxCheckInterval + jitter
	if lastFailed {
		wait = singboxRetryInterval
	}
	return !now.Before(last.Add(wait))
}

// singboxJitterFor derives this machine's stable offset within the jitter
// window from its hostname.
func singboxJitterFor(host string, window time.Duration) time.Duration {
	if window <= 0 {
		return 0
	}
	sum := sha256.Sum256([]byte(host))
	return time.Duration(binary.BigEndian.Uint64(sum[:8]) % uint64(window))
}

// formatSingboxCheck / parseSingboxCheck are the on-disk shape: a unix second
// and the outcome. Anything unparseable reads as "never checked", so a
// truncated or hand-edited file makes the client check again rather than sulk.
func formatSingboxCheck(at time.Time, ok bool) string {
	outcome := "fail"
	if ok {
		outcome = "ok"
	}
	return strconv.FormatInt(at.Unix(), 10) + " " + outcome + "\n"
}

func parseSingboxCheck(s string) (at time.Time, failed bool, ok bool) {
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return time.Time{}, false, false
	}
	sec, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return time.Time{}, false, false
	}
	return time.Unix(sec, 0), fields[1] != "ok", true
}

func (c *statusBarController) singboxCheckStatePath() string {
	_, paths := c.loadConfig()
	return filepath.Join(paths.RunDir, singboxCheckStateFile)
}

func (c *statusBarController) singboxCheckState() (last time.Time, failed bool) {
	data, err := os.ReadFile(c.singboxCheckStatePath())
	if err != nil {
		return time.Time{}, false
	}
	at, f, ok := parseSingboxCheck(string(data))
	if !ok {
		return time.Time{}, false
	}
	return at, f
}

func (c *statusBarController) recordSingboxCheck(ok bool) {
	path := c.singboxCheckStatePath()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(formatSingboxCheck(time.Now(), ok)), 0o644)
}

// startSingboxUpdateLoop runs a check now and then keeps asking on a schedule
// for as long as the app is open.
func (c *statusBarController) startSingboxUpdateLoop() {
	safeGo(func() {
		t := time.NewTicker(singboxTickInterval)
		defer t.Stop()
		for {
			c.singboxUpdateTick()
			<-t.C
		}
	})
}

// singboxUpdateTick checks when one is due and the moment is not a bad one.
func (c *statusBarController) singboxUpdateTick() {
	last, failed := c.singboxCheckState()
	host, _ := os.Hostname()
	// A missing binary is not "checked recently", it is "the app cannot work":
	// the schedule is about picking up new builds, and there is nothing to pick
	// up from.
	if !c.singboxMissing() && !singboxCheckDue(last, failed, singboxJitterFor(host, singboxCheckJitter), time.Now()) {
		return
	}
	// The download puts a window on screen. Landing it on top of a panel the
	// user is typing into, or over a connect they are watching, is exactly the
	// interruption a background updater should avoid — the next tick will do it.
	if c.busyForSingboxUpdate() {
		return
	}
	c.autoUpdateSingboxIfNeeded()
}

// busyForSingboxUpdate reports whether the user is in the middle of something
// the update window would interrupt.
func (c *statusBarController) busyForSingboxUpdate() bool {
	if _, inProgress := c.currentAction(); inProgress {
		return true
	}
	return c.panelOpen()
}

// panelOpen reads the open-panel fields on the main thread, where they are
// written; reading them straight from this goroutine would be a data race.
func (c *statusBarController) panelOpen() bool {
	open := make(chan bool, 1)
	applekit.RunOnMain(func() {
		open <- c.singboxP != nil || c.routeP != nil
	})
	select {
	case v := <-open:
		return v
	case <-time.After(2 * time.Second):
		// The main thread is wedged or the app is shutting down; treat that as
		// busy rather than popping a window into an unknown UI state.
		return true
	}
}

// singboxMissing reports whether the sing-box binary the client would run is
// absent. Checked against the same directory the installer writes to and the
// updater downloads into.
func (c *statusBarController) singboxMissing() bool {
	_, paths := c.loadConfig()
	return singboxMissingIn(paths.BinDir, exec.LookPath)
}

// singboxMissingIn is the directory check on its own. lookPath is a parameter
// rather than a call to exec.LookPath, so a test can say what is on PATH
// instead of inheriting whatever the machine happens to have: with an old
// install still on PATH the check answered "not missing" for any directory,
// which made the test pass or fail on the state of the machine running it —
// and machines with an old install are exactly the ones this check is for.
func singboxMissingIn(binDir string, lookPath func(string) (string, error)) bool {
	if st, err := os.Stat(filepath.Join(binDir, "sing-box")); err == nil && !st.IsDir() {
		return false
	}
	// A sing-box from PATH (brew, a developer's own build) counts too.
	if lookPath != nil {
		if _, err := lookPath("sing-box"); err == nil {
			return false
		}
	}
	return true
}
