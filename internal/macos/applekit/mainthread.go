//go:build darwin

package applekit

import (
	"fmt"
	"os"
	"runtime/debug"
	"sync"
)

// AppKit requires every UI mutation to happen on the main thread (the one
// running NSApplication.run). Background goroutines that finish a subprocess and
// need to update the menu marshal their UI work back through RunOnMain, which is
// the purego equivalent of dispatch_async(dispatch_get_main_queue(), ^{ … }).

var (
	mainMu      sync.Mutex
	pendingMain []func()
)

// RunOnMain schedules fn to run on the main thread and returns immediately. It
// is safe to call from any goroutine. Each call enqueues one closure and asks
// AppKit to invoke the dispatcher's akRunMain: on the main thread via
// -performSelectorOnMainThread:withObject:waitUntilDone:, which drains exactly
// one closure — so enqueues and drains stay balanced even under concurrency.
func RunOnMain(fn func()) {
	if fn == nil {
		return
	}
	mainMu.Lock()
	pendingMain = append(pendingMain, fn)
	mainMu.Unlock()

	d := actionDispatcher()
	d.Send(Sel("performSelectorOnMainThread:withObject:waitUntilDone:"),
		Sel("akRunMain:"), nilID, false)
}

// drainOneMainFunc pops and runs the next queued closure. Called only from the
// main thread by the dispatcher's akRunMain: handler.
func drainOneMainFunc() {
	mainMu.Lock()
	var fn func()
	if len(pendingMain) > 0 {
		fn = pendingMain[0]
		pendingMain = pendingMain[1:]
	}
	mainMu.Unlock()
	if fn != nil {
		// Recover so a panic in a marshaled UI update never crashes the process.
		defer recoverPanic()
		fn()
	}
}

// recoverPanic keeps a panic in a callback from taking the process down. It
// must be deferred directly: recover only stops a panic from there.
func recoverPanic() {
	if r := recover(); r != nil {
		fmt.Fprintf(os.Stderr, "panic in callback: %v\n%s", r, debug.Stack())
	}
}
