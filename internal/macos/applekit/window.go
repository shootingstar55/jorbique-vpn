//go:build darwin

package applekit

import "sync"

// Window wraps an NSWindow/NSPanel used for the modal-style dialogs. Phase 3
// panels have no text input, so a plain NSPanel suffices; the custom
// key-command subclass (for ⌘V routing into text fields) arrives in Phase 4.
type Window struct{ id ID }

// Window style masks and levels.
const (
	styleTitled       = 1 << 0 // NSWindowStyleMaskTitled
	styleClosable     = 1 << 1 // NSWindowStyleMaskClosable
	styleMiniaturize  = 1 << 2 // NSWindowStyleMaskMiniaturizable
	styleResizable    = 1 << 3 // NSWindowStyleMaskResizable
	backingBuffered   = 2      // NSBackingStoreBuffered
	floatingWindowLvl = 3      // NSFloatingWindowLevel
)

// The masks the panels are built with. Named because they decide which title-bar
// buttons exist, and because making a window resizable rewrites the whole mask —
// which is how the miniaturise button went missing the first time.
const (
	panelStyleMask          = styleTitled | styleClosable | styleMiniaturize
	resizablePanelStyleMask = panelStyleMask | styleResizable
)

// NewPanel creates a titled, closable floating NSPanel of the given content
// size, themed dark to match the menu. It is not released when closed (the Go
// owner controls its lifetime via Close/Release). Ports the panel setup shared
// by VPNSelectorPanel/StopSelectorPanel.
func NewPanel(width, height CGFloat, title string) Window {
	return newPanelOfClass("NSPanel", width, height, title)
}

// newPanelOfClass builds a titled/closable floating panel of the named class,
// shared by NewPanel and the key-command panel subclass.
func newPanelOfClass(className string, width, height CGFloat, title string) Window {
	rect := NewRect(0, 0, width, height)
	p := cls(className, "alloc").Send(Sel("initWithContentRect:styleMask:backing:defer:"),
		rect, panelStyleMask, backingBuffered, false)
	w := Window{id: p}
	p.Send(Sel("setTitle:"), nsString(title))
	p.Send(Sel("setReleasedWhenClosed:"), false)
	p.Send(Sel("setLevel:"), floatingWindowLvl)
	p.Send(Sel("setHidesOnDeactivate:"), false)
	// A window left on another desktop should come to the one the user is
	// looking at, rather than making the click switch Spaces under them.
	p.Send(Sel("setCollectionBehavior:"), uint(collectionMoveToActiveSpace))
	w.prepareDark()
	return w
}

// titleHidden is NSWindowTitleHidden.
const titleHidden = 1

// collectionMoveToActiveSpace is NSWindowCollectionBehaviorMoveToActiveSpace.
const collectionMoveToActiveSpace = 1 << 1

// prepareDark applies the dark appearance and modal background. Ports
// uiDesignPrepareModalWindow.
func (w Window) prepareDark() {
	appearance := cls("NSAppearance", "appearanceNamed:", nsString("NSDarkAqua"))
	w.id.Send(Sel("setAppearance:"), appearance)
	// Forcing the dark appearance is not enough on its own: the title bar is drawn by the
	// window's frame view, which paints its own material and came out light grey against the
	// slate body on a light-mode system. Making it transparent and giving the *window* (not
	// just the content view) the panel colour turns the whole thing into one dark surface,
	// which is also what the Windows client's chromeless panels look like.
	w.id.Send(Sel("setTitlebarAppearsTransparent:"), true)
	w.id.Send(Sel("setBackgroundColor:"), modalBgColor().id)
	// The title text keeps the light-mode treatment even under a forced dark appearance, so
	// on the now-dark bar it came out near-black. Hide it: the panels are opened from a named
	// menu item and carry their own headings, exactly as the Windows client's modals do.
	w.id.Send(Sel("setTitleVisibility:"), titleHidden)
	w.ContentView().SetBackground(modalBgColor())
}

// ContentView returns the window's content view.
func (w Window) ContentView() View { return contentViewOf(w.id) }

// SetResizable lets the user resize the window (adds the resizable style mask).
// Combine with autoresizing masks / Auto Layout on the content so subviews reflow.
func (w Window) SetResizable() {
	w.id.Send(Sel("setStyleMask:"), uint(resizablePanelStyleMask))
}

// SetNormalLevel takes the window out of the floating level.
//
// A floating window sits above everything else, which is right for a palette
// the user is answering and wrong for the app's own window — and the Dock does
// not miniaturise one that floats, so the yellow button did nothing on it.
func (w Window) SetNormalLevel() { w.id.Send(Sel("setLevel:"), 0) }

// SetContentMinSize clamps how small the user can drag the content area.
func (w Window) SetContentMinSize(width, height CGFloat) {
	w.id.Send(Sel("setContentMinSize:"), Size{Width: width, Height: height})
}

// AddSubview adds a view to the content view.
func (w Window) AddSubview(v View) { w.ContentView().addSubview(v.id) }

// Center positions the window on screen and brings it to the front, activating
// the (accessory) app so the panel can take key focus. Ports the
// center/makeKeyAndOrderFront pair plus the activation an accessory app needs.
func (w Window) Center() { w.id.Send(Sel("center")) }

// Show brings the panel to the front as the key window, from wherever it was.
//
// Three things can leave a window behind after a menu click, and
// makeKeyAndOrderFront answers none of them on its own: it is minimised into the
// Dock, it is on another Space, or the app — an accessory with no Dock icon of
// its own — is simply not the active one. So the window is de-miniaturised, the
// app activated, and the window ordered front regardless of activation, which
// covers the case where the activation is refused.
func (w Window) Show() {
	if intMsg(w.id, "isMiniaturized") != 0 {
		w.id.Send(Sel("deminiaturize:"), nilID)
	}
	app := cls("NSApplication", "sharedApplication")
	// -activate is the current spelling and -activateIgnoringOtherApps: the one
	// before it; older systems have only the second, newer ones increasingly
	// ignore it. Whichever is there answers.
	if intMsg(app, "respondsToSelector:", Sel("activate")) != 0 {
		app.Send(Sel("activate"))
	} else {
		app.Send(Sel("activateIgnoringOtherApps:"), true)
	}
	w.id.Send(Sel("makeKeyAndOrderFront:"), nilID)
	w.id.Send(Sel("orderFrontRegardless"))
}

// Close closes the window (it is not freed, since releasedWhenClosed is NO).
func (w Window) Close() { w.id.Send(Sel("close")) }

// Release drops the alloc-owned reference once the window is finished with. ARC
// no longer applies, so the Go owner must balance the +1 from alloc/init.
func (w Window) Release() { w.id.Send(Sel("release")) }

// OnClose registers fn to run when the window closes (e.g. the user clicks the
// title-bar close button), so the owner can drop its reference. Implemented with
// a shared NSWindowDelegate whose windowWillClose: looks the window up by id.
func (w Window) OnClose(fn func()) {
	winCloseMu.Lock()
	winCloseByID[w.id] = fn
	winCloseMu.Unlock()
	w.id.Send(Sel("setDelegate:"), windowDelegate())
}

// modalBgColor is the slate-900 modal chrome fill (uiWinModalBg).
func modalBgColor() Color { return srgb(15, 23, 42, 1) }

// ---------------------------------------------------------------------------
// Shared window-close delegate.
// ---------------------------------------------------------------------------

var (
	winCloseMu     sync.Mutex
	winCloseByID   = map[ID]func(){}
	windowDelegID  ID
	windowDelgOnce sync.Once
)

// windowDelegate returns the shared NSWindowDelegate, registering its class on
// first use. windowWillClose: fires the registered closure (if any) and forgets
// the window.
func windowDelegate() ID {
	windowDelgOnce.Do(func() {
		_, err := registerClass("AKWindowDelegate", "NSObject", []method{{
			cmd: Sel("windowWillClose:"),
			fn: func(self ID, _ SEL, note ID) {
				win := send(note, "object")
				winCloseMu.Lock()
				fn := winCloseByID[win]
				delete(winCloseByID, win)
				winCloseMu.Unlock()
				if fn != nil {
					fn()
				}
			},
		}})
		if err != nil {
			panic(err)
		}
		windowDelegID = cls("AKWindowDelegate", "alloc").Send(Sel("init"))
	})
	return windowDelegID
}
