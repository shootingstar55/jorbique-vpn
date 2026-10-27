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

// A jorbique:// link reaches the registered handler as a plain string, and one
// that arrives before the handler exists waits for it.
func TestOpenURLsReachesTheHandler(t *testing.T) {
	if err := Load(); err != nil {
		t.Skipf("AppKit unavailable: %v", err)
	}
	t.Cleanup(func() {
		appReopenMu.Lock()
		appOpenURLsFn = nil
		appOpenURLsQueue = nil
		appReopenMu.Unlock()
	})
	link := "jorbique://import?uuid=0b5e0f3a-1c2d-4e5f-8a9b-0c1d2e3f4a5b"
	urls := nsArray(cls("NSURL", "URLWithString:", nsString(link)))
	deleg := appDelegate()
	deleg.Send(Sel("application:openURLs:"), nilID, urls)

	var got []string
	app, err := NewApp()
	if err != nil {
		t.Skipf("NSApplication unavailable: %v", err)
	}
	app.OnOpenURLs(func(u []string) { got = append(got, u...) })
	if len(got) != 1 || got[0] != link {
		t.Fatalf("queued link = %q, want [%q]", got, link)
	}
	deleg.Send(Sel("application:openURLs:"), nilID, urls)
	if len(got) != 2 {
		t.Fatalf("handler saw %d links, want 2", len(got))
	}
}

// Quit from the Dock is one applicationShouldTerminate: callback; the
// registered function decides, and a quit it does not hold goes through.
func TestShouldTerminateAsksTheRegisteredFunction(t *testing.T) {
	if err := Load(); err != nil {
		t.Skipf("AppKit unavailable: %v", err)
	}
	t.Cleanup(func() {
		appReopenMu.Lock()
		appShouldTerminateFn = nil
		appReopenMu.Unlock()
	})
	deleg := appDelegate()
	ask := func() int { return intMsg(deleg, "applicationShouldTerminate:", nilID) }

	if got := ask(); got != int(terminateNow) {
		t.Errorf("no handler: reply = %d, want NSTerminateNow", got)
	}
	var sawSessionEnd []bool
	appReopenMu.Lock()
	appShouldTerminateFn = func(sessionEnd bool) bool {
		sawSessionEnd = append(sawSessionEnd, sessionEnd)
		return false
	}
	appReopenMu.Unlock()
	if got := ask(); got != int(terminateCancel) {
		t.Errorf("held quit: reply = %d, want NSTerminateCancel", got)
	}
	// Called directly, with no quit Apple Event in flight: not a session end.
	if len(sawSessionEnd) != 1 || sawSessionEnd[0] {
		t.Errorf("handler saw sessionEnd = %v, want [false]", sawSessionEnd)
	}
}
