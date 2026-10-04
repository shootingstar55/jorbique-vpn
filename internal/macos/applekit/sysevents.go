//go:build darwin

package applekit

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/ebitengine/purego"
)

// This file is the purego port of the cgo VPN daemon's system-event watchers:
// SCDynamicStore network-change notifications and IOKit sleep/wake power
// notifications, both driven by a CFRunLoop. It is plain C-ABI (no Objective-C
// messaging) plus two Go callbacks installed via purego.NewCallback — the
// "easy" framework layer the migration plan flagged as low risk. It lets the
// LaunchDaemon run under CGO_ENABLED=0.

// IOKit power-management message types (iokit_common_msg values).
const (
	kIOMessageCanSystemSleep     = 0xE0000270
	kIOMessageSystemWillSleep    = 0xE0000280
	kIOMessageSystemHasPoweredOn = 0xE0000300
)

var (
	cfRunLoopGetCurrent  func() uintptr
	cfRunLoopAddSource   func(rl, source, mode uintptr)
	cfRunLoopRun         func()
	cfRunLoopStop        func(rl uintptr)
	cfArrayCreateMutable func(alloc uintptr, capacity int, callbacks uintptr) uintptr
	cfArrayAppendValue   func(arr, value uintptr)

	scDynamicStoreCreate              func(alloc, name, callout uintptr, context *[5]uintptr) uintptr
	scDynamicStoreSetNotificationKeys func(store, keys, patterns uintptr) bool
	scDynamicStoreCreateRunLoopSource func(alloc, store uintptr, order int) uintptr

	ioRegisterForSystemPower           func(refcon uintptr, thePortRef *uintptr, callback uintptr, notifier *uint32) uint32
	ioAllowPowerChange                 func(kernPort, notificationID uintptr) int32
	ioNotificationPortGetRunLoopSource func(notify uintptr) uintptr

	cfArrayCallBacks uintptr // &kCFTypeArrayCallBacks
	runLoopDefault   uintptr // kCFRunLoopDefaultMode (value-constructed)
)

var sysOnce sync.Once

// daemonRunLoop holds the CFRunLoop driven by RunCFRunLoop so StopCFRunLoop can
// stop it from another goroutine (CFRunLoopStop is thread-safe).
var daemonRunLoop atomic.Uintptr

func initSysevents() {
	initCF() // brings up LoadCABI + the shared CF helpers (cfStr/cfRelease)
	sysOnce.Do(func() {
		purego.RegisterLibFunc(&cfRunLoopGetCurrent, coreFoundation, "CFRunLoopGetCurrent")
		purego.RegisterLibFunc(&cfRunLoopAddSource, coreFoundation, "CFRunLoopAddSource")
		purego.RegisterLibFunc(&cfRunLoopRun, coreFoundation, "CFRunLoopRun")
		purego.RegisterLibFunc(&cfRunLoopStop, coreFoundation, "CFRunLoopStop")
		purego.RegisterLibFunc(&cfArrayCreateMutable, coreFoundation, "CFArrayCreateMutable")
		purego.RegisterLibFunc(&cfArrayAppendValue, coreFoundation, "CFArrayAppendValue")
		purego.RegisterLibFunc(&scDynamicStoreCreate, systemConfiguration, "SCDynamicStoreCreate")
		purego.RegisterLibFunc(&scDynamicStoreSetNotificationKeys, systemConfiguration, "SCDynamicStoreSetNotificationKeys")
		purego.RegisterLibFunc(&scDynamicStoreCreateRunLoopSource, systemConfiguration, "SCDynamicStoreCreateRunLoopSource")
		purego.RegisterLibFunc(&ioRegisterForSystemPower, ioKit, "IORegisterForSystemPower")
		purego.RegisterLibFunc(&ioAllowPowerChange, ioKit, "IOAllowPowerChange")
		purego.RegisterLibFunc(&ioNotificationPortGetRunLoopSource, ioKit, "IONotificationPortGetRunLoopSource")

		cfArrayCallBacks = mustSym(coreFoundation, "kCFTypeArrayCallBacks")
		runLoopDefault = cfStrConst("kCFRunLoopDefaultMode")
	})
}

// InstallNetworkWatcher subscribes to global IPv4/IPv6 changes via SCDynamicStore
// and calls onChange (on the run-loop thread) when they occur. The run loop must
// then be driven via RunCFRunLoop. Ports InstallNetworkWatcher.
func InstallNetworkWatcher(onChange func()) error {
	initSysevents()
	cb := purego.NewCallback(func(store, changedKeys, info uintptr) {
		defer recoverPanic()
		onChange()
	})
	var ctx [5]uintptr // zeroed SCDynamicStoreContext (no info/retain/release)
	store := scDynamicStoreCreate(0, cfStrConst("jorbique-vpn"), cb, &ctx)
	if store == 0 {
		return errors.New("SCDynamicStoreCreate failed")
	}
	patterns := cfArrayCreateMutable(0, 2, cfArrayCallBacks)
	for _, key := range []string{"State:/Network/Global/IPv4", "State:/Network/Global/IPv6"} {
		k := cfStr(key)
		cfArrayAppendValue(patterns, k)
		cfRelease(k) // the array retains it
	}
	scDynamicStoreSetNotificationKeys(store, 0, patterns)
	cfRelease(patterns)

	src := scDynamicStoreCreateRunLoopSource(0, store, 0)
	if src == 0 {
		return errors.New("SCDynamicStoreCreateRunLoopSource failed")
	}
	cfRunLoopAddSource(cfRunLoopGetCurrent(), src, runLoopDefault)
	// The run loop now retains src; drop our Create-rule reference so it isn't
	// leaked. store is deliberately kept: it backs the scheduled source and must
	// live for the daemon's lifetime (released implicitly when the process exits).
	cfRelease(src)
	return nil
}

// InstallPowerWatcher subscribes to system sleep/wake via IOKit and calls
// onWillSleep before acknowledging sleep, and onWake after power-on. The run
// loop must then be driven via RunCFRunLoop. Ports InstallPowerWatcher +
// powerCallback.
func InstallPowerWatcher(onWillSleep, onWake func()) error {
	initSysevents()
	var rootPort uintptr
	cb := purego.NewCallback(func(refcon, service, messageType, messageArgument uintptr) {
		defer recoverPanic()
		switch uint32(messageType) {
		case kIOMessageSystemWillSleep:
			// Do our work (DNS restore, pause checks) before allowing sleep.
			onWillSleep()
			ioAllowPowerChange(rootPort, messageArgument)
		case kIOMessageCanSystemSleep:
			ioAllowPowerChange(rootPort, messageArgument)
		case kIOMessageSystemHasPoweredOn:
			onWake()
		}
	})
	var portRef uintptr
	var notifier uint32
	rp := ioRegisterForSystemPower(0, &portRef, cb, &notifier)
	if rp == 0 || portRef == 0 {
		return errors.New("IORegisterForSystemPower failed")
	}
	rootPort = uintptr(rp)
	src := ioNotificationPortGetRunLoopSource(portRef)
	if src == 0 {
		return errors.New("IONotificationPortGetRunLoopSource failed")
	}
	cfRunLoopAddSource(cfRunLoopGetCurrent(), src, runLoopDefault)
	return nil
}

// RunCFRunLoop runs the current thread's CFRunLoop, servicing the installed
// watchers. It blocks until the run loop is stopped via StopCFRunLoop (e.g. on
// a shutdown signal). Ports RunDaemonLoop.
func RunCFRunLoop() {
	initSysevents()
	daemonRunLoop.Store(cfRunLoopGetCurrent())
	cfRunLoopRun()
}

// StopCFRunLoop stops the run loop started by RunCFRunLoop, causing it to
// return. Safe to call from any goroutine/thread (CFRunLoopStop is thread-safe).
// No-op if the run loop has not started yet.
func StopCFRunLoop() {
	if rl := daemonRunLoop.Load(); rl != 0 {
		cfRunLoopStop(rl)
	}
}
