package singleinstance

import "testing"

func TestTryMutex(t *testing.T) {
	release, ok := TryMutex()
	if !ok {
		t.Error("TryMutex should succeed on this platform")
	}
	if release == nil {
		t.Fatal("release func must not be nil")
	}
	release() // must not panic
}
