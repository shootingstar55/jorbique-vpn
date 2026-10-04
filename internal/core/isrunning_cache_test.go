package core

import (
	"sync"
	"testing"
	"time"
)

// TestCachedIsRunningTTL covers the cachedIsRunning() hit/miss boundaries: a fresh value is served,
// an aged value (past isRunningCacheTTL) is a miss, and an invalidated (zero-timestamp) cache is a miss.
func TestCachedIsRunningTTL(t *testing.T) {
	t.Cleanup(invalidateIsRunningCache)

	isRunningMu.Lock()
	isRunningCached = true
	isRunningCachedAt = time.Now()
	isRunningMu.Unlock()

	if v, ok := cachedIsRunning(); !ok || !v {
		t.Fatalf("fresh cache should be served: v=%v ok=%v", v, ok)
	}

	// Age the cache past the TTL -> miss.
	isRunningMu.Lock()
	isRunningCachedAt = time.Now().Add(-2 * isRunningCacheTTL)
	isRunningMu.Unlock()
	if _, ok := cachedIsRunning(); ok {
		t.Fatal("expired cache should report a miss")
	}

	// Zero timestamp (never populated / just invalidated) -> miss.
	invalidateIsRunningCache()
	if _, ok := cachedIsRunning(); ok {
		t.Fatal("invalidated cache should report a miss")
	}
}

// TestInvalidateIsRunningCacheBumpsEpoch verifies the publish-guard primitive: every invalidate
// bumps isRunningEpoch (so a slow in-flight IsRunning() computation cannot republish stale state)
// and zeroes the timestamp (so the next read recomputes).
func TestInvalidateIsRunningCacheBumpsEpoch(t *testing.T) {
	t.Cleanup(invalidateIsRunningCache)

	isRunningMu.Lock()
	isRunningCached = true
	isRunningCachedAt = time.Now()
	before := isRunningEpoch
	isRunningMu.Unlock()

	invalidateIsRunningCache()

	isRunningMu.Lock()
	after := isRunningEpoch
	at := isRunningCachedAt
	isRunningMu.Unlock()

	if after != before+1 {
		t.Fatalf("epoch not bumped exactly once: before=%d after=%d", before, after)
	}
	if !at.IsZero() {
		t.Fatal("invalidate should zero the cache timestamp so the next read recomputes")
	}
}

// TestIsRunningConcurrentInvalidate hammers IsRunning() and invalidateIsRunningCache() from many
// goroutines so the race detector exercises isRunningMu / isRunningComputeMu / isRunningEpoch under
// contention (the scenario the cache+epoch design exists to make safe). Run with -race.
func TestIsRunningConcurrentInvalidate(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir()) // isRunningRaw reads paths.DataDir(); keep it empty + deterministic
	invalidateIsRunningCache()

	const readers, invalidators = 8, 4
	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = IsRunning()
				}
			}
		}()
	}
	for i := 0; i < invalidators; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					invalidateIsRunningCache()
				}
			}
		}()
	}

	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()

	// No sing-box exists on the stub platform; after the churn the state must converge to not-running.
	if IsRunning() {
		t.Error("expected not running on the stub platform")
	}
}
