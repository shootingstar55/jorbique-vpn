package applekit

import "testing"

// A click on the Dock icon reaches the registered command. The click itself
// needs a Dock and a running app, but what it turns into is one delegate
// callback — this drives that callback directly, which is the part that broke
// when the app had no delegate at all.
func TestReopenRunsTheRegisteredCommand(t *testing.T) {
	if err := Load(); err != nil {
		t.Skipf("AppKit unavailable: %v", err)
	}
	called := 0
	appReopenMu.Lock()
	appReopenFn = func() { called++ }
	appReopenMu.Unlock()
	t.Cleanup(func() {
		appReopenMu.Lock()
		appReopenFn = nil
		appReopenMu.Unlock()
	})

	deleg := appDelegate()
	if deleg == nilID {
		t.Fatal("app delegate was not created")
	}
	if got := intMsg(deleg, "applicationShouldHandleReopen:hasVisibleWindows:", nilID, false); got != 0 {
		t.Errorf("applicationShouldHandleReopen: = %d, want 0 (NO — the command already opened the window)", got)
	}
	if called != 1 {
		t.Errorf("registered command ran %d times, want 1", called)
	}
}
