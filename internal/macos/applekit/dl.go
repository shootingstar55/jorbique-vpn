//go:build darwin

// Package applekit is the low-level, reusable bridge from Go to the macOS
// system frameworks, using ebitengine/purego instead of cgo. It contains no
// business logic: every AppKit/Foundation call site in the UI goes through a
// typed wrapper here, so the one genuinely unsafe layer (raw objc_msgSend and
// dynamic class creation) stays localized to a single reviewable package.
//
// Building requires CGO_ENABLED=0 GOOS=darwin; at runtime the frameworks are
// dlopen'd from their standard system locations, which exist on every Mac.
package applekit

import (
	"fmt"
	"sync"

	"github.com/ebitengine/purego"
)

// Framework handles, populated by Load. They are process-global and never
// closed: the frameworks live for the lifetime of the menu-bar app.
var (
	foundation          uintptr
	appKit              uintptr
	webKit              uintptr
	systemConfiguration uintptr
	ioKit               uintptr
	coreFoundation      uintptr
	libSystem           uintptr // libdispatch + C runtime symbols
)

const appKitPath = "/System/Library/Frameworks/AppKit.framework/AppKit"
const webKitPath = "/System/Library/Frameworks/WebKit.framework/WebKit"

// cabiFrameworks are the C-ABI / Foundation frameworks needed for
// SystemConfiguration, IOKit and CoreFoundation work. They deliberately exclude
// AppKit: the VPN LaunchDaemon (which uses the SC/IOKit watchers) runs headless as root with no window-server session, where loading AppKit is
// pointless and potentially harmful. Using absolute paths avoids any dependence
// on dyld search order.
var cabiFrameworks = []struct {
	name   string
	target *uintptr
}{
	{"/System/Library/Frameworks/Foundation.framework/Foundation", &foundation},
	{"/System/Library/Frameworks/SystemConfiguration.framework/SystemConfiguration", &systemConfiguration},
	{"/System/Library/Frameworks/IOKit.framework/IOKit", &ioKit},
	{"/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", &coreFoundation},
	{"/usr/lib/libSystem.B.dylib", &libSystem},
}

const dlMode = purego.RTLD_GLOBAL | purego.RTLD_NOW

var fwMu sync.Mutex

// ensureFramework dlopen's path into *target once (idempotent, concurrency-safe).
func ensureFramework(path string, target *uintptr) error {
	fwMu.Lock()
	defer fwMu.Unlock()
	if *target != 0 {
		return nil
	}
	h, err := purego.Dlopen(path, dlMode)
	if err != nil {
		return fmt.Errorf("applekit: dlopen %s: %w", path, err)
	}
	*target = h
	return nil
}

// LoadCABI loads only the C-ABI / Foundation frameworks (no AppKit), for headless
// users like the VPN daemon. Safe to call repeatedly.
func LoadCABI() error {
	for _, fw := range cabiFrameworks {
		if err := ensureFramework(fw.name, fw.target); err != nil {
			return err
		}
	}
	return nil
}

// Load loads every framework the menu-bar UI needs, including AppKit. A non-nil
// error means a framework is missing, which on a stock macOS install should
// never happen — surface it and bail rather than continue into guaranteed
// objc_msgSend crashes.
func Load() error {
	if err := LoadCABI(); err != nil {
		return err
	}
	return ensureFramework(appKitPath, &appKit)
}

func loadWebKit() error { return ensureFramework(webKitPath, &webKit) }

// mustSym resolves the address of an exported C symbol from an already-loaded
// framework handle, panicking if absent. For data symbols (e.g. the
// CFDictionary callback-table globals) the returned address is the pointer the
// API expects — no dereference, so no unsafe conversion. Absence of a documented
// system symbol is a programming error or an unsupported OS, neither
// recoverable, so panicking at registration gives the clearest diagnostic.
func mustSym(handle uintptr, name string) uintptr {
	sym, err := purego.Dlsym(handle, name)
	if err != nil {
		panic(fmt.Sprintf("applekit: missing symbol %s: %v", name, err))
	}
	return sym
}
