package netsetup

import (
	"context"
	"testing"
	"time"
)

func TestWaitForConnectionImmediate(t *testing.T) {
	calls := 0
	if !waitForConnection(context.Background(), 5, func() bool { calls++; return true }) {
		t.Fatal("expected true when already connected")
	}
	if calls != 1 {
		t.Fatalf("expected a single check before returning, got %d", calls)
	}
}

func TestWaitForConnectionZeroTimeoutChecksOnce(t *testing.T) {
	// Behavioral change: timeoutSeconds==0 now performs one connectivity check instead of
	// returning false unconditionally.
	if !waitForConnection(context.Background(), 0, func() bool { return true }) {
		t.Fatal("zero timeout with connected=true must return true")
	}
	if waitForConnection(context.Background(), 0, func() bool { return false }) {
		t.Fatal("zero timeout with connected=false must return false")
	}
}

func TestWaitForConnectionContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before the wait starts

	calls := 0
	start := time.Now()
	if waitForConnection(ctx, 60, func() bool { calls++; return false }) {
		t.Fatal("expected false when the context is cancelled before connecting")
	}
	if calls != 1 {
		t.Fatalf("expected one check before honoring ctx.Done(), got %d", calls)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("cancellation did not short-circuit the wait (took %s)", elapsed)
	}
}

func TestWaitForConnectionFinalCheckAfterTimeout(t *testing.T) {
	// After the poll loop exhausts timeoutSeconds a final check runs, so a network that comes up on
	// the last tick is still reported connected.
	calls := 0
	if !waitForConnection(context.Background(), 1, func() bool {
		calls++
		return calls >= 2 // false during the loop, true on the post-loop check
	}) {
		t.Fatal("expected the post-loop final check to report connected")
	}
}

func TestWaitForConnectionNilContext(t *testing.T) {
	// A nil context must default to context.Background() rather than panic.
	var ctx context.Context
	if !waitForConnection(ctx, 1, func() bool { return true }) {
		t.Fatal("nil context with connected=true must return true")
	}
}
