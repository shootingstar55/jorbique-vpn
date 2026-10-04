package vpnmgr

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// Starting one VPN is not something two processes may do at once, and until
// this existed they could. The menu bar's "Start" is a request the daemon
// answers by running the client as a separate process, while the daemon's own
// autostart-check brings saved tunnels back inside the daemon itself — two
// processes, so the in-process guard on that check (isAutoCheckRunning) never
// saw the other one, and startOne's "is it already running?" test was a read
// with nothing holding still behind it.
//
// What that cost: a batch start records what it started only once the batch is
// over, so for the minutes a browser login or an approval in a bot takes, the
// check sees a VPN that is neither running nor on the started list and starts it
// again. The result is two sing-box processes for one profile, two pid files
// written to the same path — leaving one process with nothing pointing at it,
// which is how a tunnel survives a Stop — and the user asked to approve the same
// login twice.
//
// One lock per VPN rather than one for all of them: the race is two starts of
// the same profile, and a single lock would also serialize the profiles that
// have no quarrel with each other, which is the wait startBatch just removed.
//
// The lock is an open file descriptor held by the kernel, so a process that
// dies mid-start leaves nothing stale behind. It lives in the run directory,
// which belongs to root — and every start goes through root, so both
// contenders look at the same file.

// errStartInProgress is what a start returns when another process holds this
// VPN's lock. It is not a failure and it is not a success: the tunnel is on its
// way up, but under someone else's attempt, and whether it arrives is that
// process's story to tell. A batch that recorded it as started would put the
// VPN on the started list — what an autostart brings back at boot — on the
// strength of a start it never saw finish.
var errStartInProgress = errors.New("another process is already starting this VPN")

// lockStart takes the start lock for one VPN. ok is false when another process
// is already starting it: the tunnel the caller wants is on its way up, and the
// answer is to leave it alone rather than to start a second copy. The caller
// reports that as errStartInProgress.
func (m *Manager) lockStart(name string) (release func(), ok bool) {
	f, err := os.OpenFile(filepath.Join(m.Paths.RunDir, name+".start.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		// Nowhere to put the lock is not a reason to refuse to start; it only
		// means this machine cannot serialize, which is where it was before.
		m.Logger.Warn("start lock for " + name + " unavailable (" + err.Error() + "); starting without it")
		return func() {
			// No lock was taken, so there is nothing to release.
		}, true
	}
	// Only EWOULDBLOCK means someone else holds it. Reading every other error as
	// contention was how a start could disappear: the caller reports contention
	// as errStartInProgress, which is deliberately neither a success nor a
	// failure and is never retried, so a broken descriptor or an exhausted lock
	// table would have taken the tunnel down with no trace anywhere.
	var lockErr error
	for {
		lockErr = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		// A signal arriving mid-call says nothing about who holds the lock.
		if !errors.Is(lockErr, syscall.EINTR) {
			break
		}
	}
	switch {
	case lockErr == nil:
	case errors.Is(lockErr, syscall.EWOULDBLOCK):
		_ = f.Close()
		return nil, false
	default:
		// Not contention: this machine cannot serialize starts right now, which
		// is where it was before the lock existed. Starting without it risks the
		// double start again; refusing to start risks no tunnel at all, and the
		// tunnel is what the user asked for.
		_ = f.Close()
		m.Logger.Warn("start lock for " + name + " could not be taken (" + lockErr.Error() + "); starting without it")
		return func() {
			// No lock was taken, so there is nothing to release.
		}, true
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, true
}
