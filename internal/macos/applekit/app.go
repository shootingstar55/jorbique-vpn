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
	editItem := NewMenuItem("Edit")
	edit := NewMenuTitled("Edit")
	edit.AddItem(menuItemSelector("Cut", "cut:", "x"))
	edit.AddItem(menuItemSelector("Copy", "copy:", "c"))
	edit.AddItem(menuItemSelector("Paste", "paste:", "v"))
	edit.AddItem(menuItemSelector("Select All", "selectAll:", "a"))
	editItem.SetSubmenu(edit)
	main.AddItem(editItem)
}

// ---------------------------------------------------------------------------
// Application delegate.
// ---------------------------------------------------------------------------

var (
	appReopenMu sync.Mutex
	appReopenFn func()
	appDelegID  ID
	appDelgOnce sync.Once
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

// appDelegate returns the shared NSApplicationDelegate, registering its class on
// first use — the same one-shot registration the window and menu delegates use.
func appDelegate() ID {
	appDelgOnce.Do(func() {
		_, err := registerClass("AKAppDelegate", "NSObject", []method{{
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
		}})
		if err != nil {
			panic(err)
		}
		appDelegID = cls("AKAppDelegate", "alloc").Send(Sel("init"))
	})
	return appDelegID
}
