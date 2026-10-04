//go:build darwin

package applekit

import "testing"

// WKWebView must be built on the main thread — WebKit traps otherwise, and a Go
// test never runs on it: the runtime owns that thread and `go test` calls this
// from a goroutine. Creating one here failed roughly one run in ten with a
// SIGTRAP out of purego, which read as a flaky package rather than as a test
// doing something it may not.
//
// What can be checked off the main thread is what the panel actually depends on:
// that WebKit loads and the class is there. Whether a view comes out of it is
// answered by the windows themselves, which build theirs on the main thread.
func TestWebKitIsAvailable(t *testing.T) {
	if err := Load(); err != nil {
		t.Skipf("AppKit is unavailable here: %v", err)
	}
	if err := loadWebKit(); err != nil {
		t.Fatalf("WebKit did not load: %v", err)
	}
	if Class("WKWebView") == 0 {
		t.Error("WKWebView is missing")
	}
	if Class("WKWebViewConfiguration") == 0 {
		t.Error("WKWebViewConfiguration is missing")
	}
}
