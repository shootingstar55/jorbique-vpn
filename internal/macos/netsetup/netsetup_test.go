package netsetup

import (
	"context"
	"testing"
	"time"
)

func TestIPv4OnInterfaceLoopback(t *testing.T) {
	if got := ipv4OnInterface("lo0"); got == "" {
		t.Skip("no loopback IPv4 on this host")
	}
}

func TestInterfaceHasIPv4Unknown(t *testing.T) {
	if interfaceHasIPv4("nonexistent-jorbiquevpn-if99") {
		t.Fatal("expected false for missing interface")
	}
}

// forceDisconnected pins the active-service cache to a service that resolves to no interface, so
// IsNetworkConnected() is deterministically false regardless of the host's real network state. The
// wait can then exercise its context-cancellation path instead of returning true on the first probe.
func forceDisconnected(t *testing.T, n *NetworkSetup) {
	t.Helper()
	const bogus = "jorbiquevpn-nonexistent-service-xyz"
	cachedActiveServiceMu.Lock()
	cachedActiveService = bogus
	cachedActiveAt = time.Now()
	cachedActiveServiceMu.Unlock()
	t.Cleanup(func() {
		cachedActiveServiceMu.Lock()
		cachedActiveService = ""
		cachedActiveAt = time.Time{}
		cachedActiveServiceMu.Unlock()
	})
	if n.IsNetworkConnected() {
		t.Skip("host resolved an interface for a bogus service; cannot force a disconnected state")
	}
}

// TestWaitForNetworkConnectionContextCancel covers the select { case <-ctx.Done(): return false }
// arm — the whole point of the context-aware refactor — for both a pre-cancelled context and a
// cancellation that fires mid-wait.
func TestWaitForNetworkConnectionContextCancel(t *testing.T) {
	n := New("")
	forceDisconnected(t, n)

	t.Run("pre-cancelled context returns promptly", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		start := time.Now()
		if n.WaitForNetworkConnection(ctx, 5) {
			t.Fatal("expected false: network never connects")
		}
		if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
			t.Fatalf("cancelled context should abort before the first 1s tick, waited %v", elapsed)
		}
	})

	t.Run("mid-wait cancel aborts before timeout", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()
		start := time.Now()
		if n.WaitForNetworkConnection(ctx, 30) {
			t.Fatal("expected false: network never connects")
		}
		// Aborting on cancel must happen long before the 30s timeout (within the first ~1s tick).
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Fatalf("mid-wait cancel should abort within ~1s, waited %v", elapsed)
		}
	})
}

// TestWaitForNetworkConnectionZeroTimeout documents the timeoutSeconds<=0 behaviour: the loop runs
// zero times and the function falls through to a single live connectivity probe rather than always
// returning false.
func TestWaitForNetworkConnectionZeroTimeout(t *testing.T) {
	n := New("")
	forceDisconnected(t, n)
	// Disconnected -> the single fall-through probe reports false (and never blocks).
	start := time.Now()
	if n.WaitForNetworkConnection(context.Background(), 0) {
		t.Fatal("expected false: disconnected host with zero timeout")
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("zero timeout must not wait, took %v", elapsed)
	}
}
