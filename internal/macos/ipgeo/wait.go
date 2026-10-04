package ipgeo

import (
	"time"

	"jorbiquevpn/internal/macos/core"
)

// WaitOpts configures dynamic exit-IP polling (no fixed single delay).
type WaitOpts struct {
	MinBeforeFirst time.Duration // wait before first Fetch (TUN / routes)
	MaxTotal       time.Duration // wall clock budget
	PollInterval   time.Duration // pause between attempts after first
	StableNeeded   int           // consecutive identical IPs required
}

// DefaultWaitOpts is used by the menu bar (`exit-ip wait`).
func DefaultWaitOpts() WaitOpts {
	return WaitOpts{
		MinBeforeFirst: 2 * time.Second,
		MaxTotal:       45 * time.Second,
		PollInterval:   1500 * time.Millisecond,
		StableNeeded:   2,
	}
}

// DefaultWaitOptsForPostStart skips an extra min delay: start already had a short settle sleep.
func DefaultWaitOptsForPostStart() WaitOpts {
	o := DefaultWaitOpts()
	o.MinBeforeFirst = 0
	o.MaxTotal = 50 * time.Second
	return o
}

// WaitStable polls Fetch until StableNeeded consecutive identical IPs, or MaxTotal elapses.
// ok is true if StableNeeded consecutive identical IPs were seen, or if the deadline passed
// but at least one valid IP was obtained (best-effort for exit-ip wait and similar callers).
func WaitStable(api core.EffectiveAPI, opt WaitOpts) (ip, cc string, ok bool) {
	if opt.StableNeeded < 1 {
		opt.StableNeeded = 1
	}
	if opt.MaxTotal < 2*time.Second {
		opt.MaxTotal = 45 * time.Second
	}
	if opt.PollInterval < 200*time.Millisecond {
		opt.PollInterval = 1500 * time.Millisecond
	}
	deadline := time.Now().Add(opt.MaxTotal)
	if opt.MinBeforeFirst > 0 {
		time.Sleep(opt.MinBeforeFirst)
	}
	var streakIP string
	streak := 0
	var lastGoodIP, lastGoodCC string
	for time.Now().Before(deadline) {
		i, c := Fetch(api)
		if i != "" && isValidIPString(i) {
			lastGoodIP, lastGoodCC = i, c
			streakIP, streak = extendStreak(streakIP, streak, i)
			if streak >= opt.StableNeeded {
				return i, c, true
			}
		} else {
			streakIP, streak = "", 0
		}
		time.Sleep(opt.PollInterval)
	}
	if lastGoodIP != "" {
		return lastGoodIP, lastGoodCC, true
	}
	return "", "", false
}

// extendStreak counts ip as one more in a row when it matches the current
// streak, and starts a new streak of one otherwise.
func extendStreak(streakIP string, streak int, ip string) (string, int) {
	if ip == streakIP {
		return streakIP, streak + 1
	}
	return ip, 1
}
