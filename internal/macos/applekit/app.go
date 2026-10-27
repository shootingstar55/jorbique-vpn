//go:build darwin

package applekit

import (
	"runtime"
	"sync"
)

// activationPolicyRegular (NSApplicationActivationPolicyRegular): a Dock icon
// and an app menu, like any other app.
const activationPolicyRegular = 0

// App wraps the shared NSApplication.
type App struct{ id ID }

// NewApp initializes the process's shared NSApplication as a menu-bar accessory
// (no Dock icon) and pins the calling goroutine to the OS thread. It must be
// called from the program's main goroutine before any other AppKit use; AppKit
// requires all UI work to happen on the main thread. Ports ui_app_init.
func NewApp() (App, error) {
	runtime.LockOSThread()
	if err := Load(); err != nil {
		return App{}, err
	}
	app := App{id: cls("NSApplication", "sharedApplication")}
	// Regular, not accessory: the client has a window of its own now, and a
	// window with no Dock icon cannot be minimised anywhere or brought back
	// from the Dock. The menu-bar item stays either way — the policy decides
	// whether there is also an icon, not whether there is a status item.
	app.id.Send(Sel("setActivationPolicy:"), activationPolicyRegular)
	return app, nil
}

// Run enters the AppKit run loop and blocks until the app terminates. Ports
// ui_run_loop.
func (a App) Run() { a.id.Send(Sel("run")) }

// Terminate asks the application to quit, as the Quit menu item does.
func (a App) Terminate() { a.id.Send(Sel("terminate:"), nilID) }

// SetMainMenu installs the application main menu (which hosts the Edit menu so
// the standard Cmd-C/V/X/A editing commands reach the focused field).
func (a App) SetMainMenu(m Menu) { a.id.Send(Sel("setMainMenu:"), m.id) }

// MainMenu returns the current main menu, or the zero Menu if none is set.
func (a App) MainMenu() Menu { return Menu{id: send(a.id, "mainMenu")} }

// InstallEditMenu installs a main-menu "Edit" submenu wiring the standard
// Cmd-X/C/V/A editing commands to the responder chain, so text fields in the
// panels get working clipboard shortcuts. Intended to be called once at startup.
// Ports uiSetupEditMenu.
func (a App) InstallEditMenu() {
	main := a.MainMenu()
	if main.id == nilID {
		main = NewMenu()
		a.SetMainMenu(main)
	}
	editItem := NewMenuItem("Правка")
	edit := NewMenuTitled("Правка")
	edit.AddItem(menuItemSelector("Вырезать", "cut:", "x"))
	edit.AddItem(menuItemSelector("Копировать", "copy:", "c"))
	edit.AddItem(menuItemSelector("Вставить", "paste:", "v"))
	edit.AddItem(menuItemSelector("Выбрать все", "selectAll:", "a"))
	editItem.SetSubmenu(edit)
	main.AddItem(editItem)
}

// ---------------------------------------------------------------------------
// Application delegate.
// ---------------------------------------------------------------------------

var (
	appReopenMu sync.Mutex
	appReopenFn func()
	// appOpenURLsFn takes the links the app is opened with; appOpenURLsQueue
	// keeps the ones that arrive before it is registered. Both under appReopenMu.
	appOpenURLsFn    func([]string)
	appOpenURLsQueue []string
	// appShouldTerminateFn decides every quit AppKit is asked for, under appReopenMu.
	appShouldTerminateFn func(sessionEnd bool) bool
	appDelegID           ID
	appDelgOnce          sync.Once
)

// OnReopen registers fn to run when the app is reopened — a click on the Dock
// icon of an app that is already running. AppKit's own answer to that click is
// to unminiaturize the app's windows, which does nothing here: the client lives
// in the menu bar and has no window until someone asks for one, so without a
// delegate the icon is the one thing on the Dock that ignores being clicked.
func (a App) OnReopen(fn func()) {
	appReopenMu.Lock()
	appReopenFn = fn
	appReopenMu.Unlock()
	a.id.Send(Sel("setDelegate:"), appDelegate())
}

// OnOpenURLs registers fn to receive the URLs the app is opened with — the
// jorbique:// links the bundle declares in CFBundleURLTypes. AppKit turns the
// GetURL Apple Event into application:openURLs: once the delegate answers it,
// whether the link launched the app or found it running. Links that came in
// before fn was registered are handed over now. fn runs on the main thread.
func (a App) OnOpenURLs(fn func([]string)) {
	appReopenMu.Lock()
	appOpenURLsFn = fn
	queued := appOpenURLsQueue
	appOpenURLsQueue = nil
	appReopenMu.Unlock()
	a.id.Send(Sel("setDelegate:"), appDelegate())
	if len(queued) > 0 && fn != nil {
		fn(queued)
	}
}

// NSApplicationTerminateReply values.
const (
	terminateCancel uint = 0
	terminateNow    uint = 1
)

// kAEQuitReason ('why?') is the attribute loginwindow puts on the quit Apple
// Event it sends at logout, restart and shutdown. A Quit from the Dock, the app
// menu or Activity Monitor comes without it.
const kAEQuitReason uint32 = 'w'<<24 | 'h'<<16 | 'y'<<8 | '?'

// OnShouldTerminate registers fn to decide whether the app may quit now. AppKit
// asks on every terminate: — the Dock's Quit, Cmd-Q, logout, Terminate itself.
// sessionEnd is true when the quit comes from logout, restart or shutdown. fn
// returns true to quit at once, false to keep running (it is then fn's job to
// quit later through Terminate). fn runs on the main thread.
func (a App) OnShouldTerminate(fn func(sessionEnd bool) bool) {
	appReopenMu.Lock()
	appShouldTerminateFn = fn
	appReopenMu.Unlock()
	a.id.Send(Sel("setDelegate:"), appDelegate())
}

// quitIsSessionEnd reports whether the Apple Event being handled is the quit
// loginwindow sends when the session ends.
func quitIsSessionEnd() bool {
	ev := send(cls("NSAppleEventManager", "sharedAppleEventManager"), "currentAppleEvent")
	if ev == nilID {
		return false
	}
	return ev.Send(Sel("attributeDescriptorForKeyword:"), kAEQuitReason) != nilID
}

// shouldTerminate is the delegate's answer to applicationShouldTerminate:. A
// panic in fn, or no fn at all, lets the quit through: an app that cannot be
// quit is worse than one that skips its cleanup.
func shouldTerminate() (reply uint) {
	reply = terminateNow
	defer recoverPanic()
	appReopenMu.Lock()
	fn := appShouldTerminateFn
	appReopenMu.Unlock()
	if fn != nil && !fn(quitIsSessionEnd()) {
		reply = terminateCancel
	}
	return reply
}

// deliverOpenURLs hands urls to the registered handler, or queues them.
func deliverOpenURLs(urls []string) {
	if len(urls) == 0 {
		return
	}
	appReopenMu.Lock()
	fn := appOpenURLsFn
	if fn == nil {
		appOpenURLsQueue = append(appOpenURLsQueue, urls...)
	}
	appReopenMu.Unlock()
	if fn != nil {
		fn(urls)
	}
}

// urlStrings reads an NSArray of NSURL into their absolute strings.
func urlStrings(array ID) []string {
	if array == nilID {
		return nil
	}
	n := intMsg(array, "count")
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		u := array.Send(Sel("objectAtIndex:"), i)
		if s := goStringFromNS(send(u, "absoluteString")); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// appDelegate returns the shared NSApplicationDelegate, registering its class on
// first use — the same one-shot registration the window and menu delegates use.
func appDelegate() ID {
	appDelgOnce.Do(func() {
		_, err := registerClass("AKAppDelegate", "NSObject", []method{{
			cmd: Sel("application:openURLs:"),
			fn: func(_ ID, _ SEL, _ ID, urls ID) {
				defer recoverPanic()
				deliverOpenURLs(urlStrings(urls))
			},
		}, {
			cmd: Sel("applicationShouldHandleReopen:hasVisibleWindows:"),
			fn: func(_ ID, _ SEL, _ ID, _ bool) bool {
				appReopenMu.Lock()
				fn := appReopenFn
				appReopenMu.Unlock()
				if fn != nil {
					fn()
				}
				// NO: the window was opened or raised just now, and the default
				// handling has nothing left to add.
				return false
			},
		}, {
			cmd: Sel("applicationShouldTerminate:"),
			fn: func(_ ID, _ SEL, _ ID) uint {
				return shouldTerminate()
			},
		}})
		if err != nil {
			panic(err)
		}
		appDelegID = cls("AKAppDelegate", "alloc").Send(Sel("init"))
	})
	return appDelegID
}
