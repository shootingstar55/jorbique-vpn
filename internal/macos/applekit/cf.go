//go:build darwin

package applekit

import (
	"strings"
	"sync"

	"github.com/ebitengine/purego"
)

// This file is the plain C-ABI half of the bridge: the CoreFoundation glue and
// the console-user lookup, called directly via purego.RegisterLibFunc with no
// Objective-C messaging.

// CFStringEncoding.
const encUTF8 uint32 = 0x08000100 // kCFStringEncodingUTF8

// C functions, registered once in initCF.
var (
	cfStringCreateWithCString func(alloc uintptr, cstr *byte, enc uint32) uintptr
	cfRelease                 func(cf uintptr)
	cfStringGetCString        func(s uintptr, buf *byte, bufSize int, enc uint32) bool

	scDynamicStoreCopyConsoleUser func(store uintptr, uid, gid *uint32) uintptr
)

var cfOnce sync.Once

// initCF loads the frameworks, registers the C functions, and builds the cached
// constants. Panics if the frameworks are unavailable (a stock macOS always has
// them; absence means an unsupported system).
func initCF() {
	cfOnce.Do(func() {
		// CF/SC/IOKit need no AppKit — use the headless-safe load so the VPN
		// daemon never pulls in the window-server-dependent AppKit.
		if err := LoadCABI(); err != nil {
			panic(err)
		}
		purego.RegisterLibFunc(&cfStringCreateWithCString, coreFoundation, "CFStringCreateWithCString")
		purego.RegisterLibFunc(&cfRelease, coreFoundation, "CFRelease")
		purego.RegisterLibFunc(&cfStringGetCString, coreFoundation, "CFStringGetCString")
		purego.RegisterLibFunc(&scDynamicStoreCopyConsoleUser, systemConfiguration, "SCDynamicStoreCopyConsoleUser")
	})
}

// cfStrConst builds a permanent CFString (never released) for a cached constant.
func cfStrConst(s string) uintptr {
	return cfStr(s)
}

// cfStr builds a CFString the caller must release.
func cfStr(s string) uintptr {
	b := append([]byte(s), 0)
	return cfStringCreateWithCString(0, &b[0], encUTF8)
}

// cfStringToGo copies a CFString into a Go string.
func cfStringToGo(s uintptr) string {
	buf := make([]byte, 512)
	if cfStringGetCString(s, &buf[0], len(buf), encUTF8) {
		return goCStr(&buf[0])
	}
	return ""
}

// ConsoleUser returns the GUI console user via SCDynamicStoreCopyConsoleUser, or
// "" when none/at the login window. Ports uiConsoleUserFromDynamicStore: it
// rejects an empty name, uid 0, and the loginwindow/_mbsetupuser placeholders so
// a root LaunchAgent does not target the wrong account.
func ConsoleUser() string {
	initCF()
	var uid, gid uint32
	cfName := scDynamicStoreCopyConsoleUser(0, &uid, &gid)
	if cfName == 0 {
		return ""
	}
	defer cfRelease(cfName)
	name := strings.TrimSpace(cfStringToGo(cfName))
	if name == "" || uid == 0 || name == "loginwindow" || name == "_mbsetupuser" {
		return ""
	}
	return name
}
