package vpnmgr

import (
	"testing"
	"time"
)

// TestNetworkWaitContextCancelsOnSleep verifies the watcher goroutine cancels the returned context
// once the machine reports it is sleeping again, so a WaitForNetworkConnection started on wake does
// not stay blocked for the full timeout.
func TestNetworkWaitContextCancelsOnSleep(t *testing.T) {
	m := &Manager{}
	m.autoMu.Lock()
	m.isSystemSleeping = true
	m.autoMu.Unlock()

	ctx, cancel := m.networkWaitContext()
	defer cancel()

	select {
	case <-ctx.Done():
		// good: the 1s watcher tick saw the machine sleeping and cancelled the wait.
	case <-time.After(3 * time.Second):
		t.Fatal("context was not cancelled after the system reported sleeping")
	}
}

// TestNetworkWaitContextStaysOpenWhileAwake verifies the context survives past a watcher tick while
// the machine is awake, and that the caller's cancel() both closes the context and stops the watcher.
func TestNetworkWaitContextStaysOpenWhileAwake(t *testing.T) {
	m := &Manager{} // isSystemSleeping == false

	ctx, cancel := m.networkWaitContext()
	defer cancel()

	select {
	case <-ctx.Done():
		t.Fatal("context cancelled while the system is awake")
	case <-time.After(1500 * time.Millisecond): // longer than the 1s watcher tick
	}

	cancel()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("context not cancelled after manual cancel")
	}
}
