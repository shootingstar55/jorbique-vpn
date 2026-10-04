//go:build windows

// Package wincom runs a COM call on a thread that is allowed to make one, and gives the caller
// a way to stop waiting for it.
//
// Two callers need this: Defender's exclusion list (WMI over root\Microsoft\Windows\Defender)
// and logon autostart (Task Scheduler). Both talk to a Windows service that can be busy or
// wedged, both run where a block would be felt - one at startup, one synchronously from
// OnStartup - and both had their own copy of the same twenty lines.
package wincom

import (
	"errors"
	"runtime"
	"sync"
	"time"

	"github.com/go-ole/go-ole"
)

// RPC_E_CHANGED_MODE: the thread is already in an apartment, which is one to work in.
const rpcChangedMode = 0x80010106

// ErrTimeout is returned when fn has not finished within the timeout. It says the caller stopped
// waiting, not that the call was cancelled: see Runner.Run.
var ErrTimeout = errors.New("the COM call did not answer in time")

// A Runner is one service's worth of COM calls: its own timeout, and its own memory of having
// timed out. One per service rather than one per package, so a wedged Defender cannot stop the
// Task Scheduler from being asked.
type Runner struct {
	Timeout time.Duration

	mu     sync.Mutex
	wedged bool
}

// Run initializes a COM apartment on a thread of its own, runs fn there, and waits for it up to
// r.Timeout.
//
// The timeout bounds the wait, not the work. A synchronous COM call cannot be cancelled from Go:
// on ErrTimeout the goroutine and its locked OS thread stay blocked in the RPC until the service
// answers, and the thread is destroyed rather than reused when the goroutine finally returns
// without unlocking it. That is the trade this makes deliberately, because a leaked thread costs
// less than a client that will not start.
//
// It is also why the second timeout is refused outright: one thread is the price of finding out
// the service is wedged, and a caller that makes several calls per refresh would otherwise pay
// it again on each one.
func (r *Runner) Run(fn func() error) error {
	r.mu.Lock()
	wedged := r.wedged
	r.mu.Unlock()
	if wedged {
		return ErrTimeout
	}

	done := make(chan error, 1)
	go func() {
		// No UnlockOSThread: on the timeout path this goroutine is still blocked, and on the
		// normal path letting the thread die with the goroutine keeps an apartment that has
		// been through CoInitializeEx out of the pool the rest of the program draws from.
		runtime.LockOSThread()
		done <- withApartment(fn)
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(r.Timeout):
		r.mu.Lock()
		r.wedged = true
		r.mu.Unlock()
		return ErrTimeout
	}
}

func withApartment(fn func() error) error {
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED|ole.COINIT_DISABLE_OLE1DDE); err != nil {
		var oleErr *ole.OleError
		if !errors.As(err, &oleErr) || oleErr.Code() != rpcChangedMode {
			return err
		}
	} else {
		defer ole.CoUninitialize()
	}
	return fn()
}
