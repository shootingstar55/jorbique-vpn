//go:build darwin

package applekit

import (
	"sync"

	"github.com/ebitengine/purego/objc"
)

// Menu wraps an NSMenu.
type Menu struct{ id ID }

// MenuItem wraps an NSMenuItem.
type MenuItem struct{ id ID }

// Control state values (NSControlStateValue).
const (
	StateOff = 0
	StateOn  = 1
)

// NewMenu creates an empty NSMenu.
func NewMenu() Menu { return Menu{id: cls("NSMenu", "alloc").Send(Sel("init"))} }

// NewMenuTitled creates an NSMenu with a title (used for submenus whose title is
// meaningful, e.g. the Edit menu).
func NewMenuTitled(title string) Menu {
	return Menu{id: cls("NSMenu", "alloc").Send(Sel("initWithTitle:"), nsString(title))}
}

// SetAutoenablesItems controls AppKit's automatic menu-item enabling. With the
// default (true), NSMenu re-enables every item that has a valid action each time
// the menu opens, clobbering manual SetEnabled(false) calls. Pass false to take
// full manual control of item enabled state.
func (m Menu) SetAutoenablesItems(v bool) { m.id.Send(Sel("setAutoenablesItems:"), v) }

// SetMinimumWidth gives a compact menu room for its connection details.
func (m Menu) SetMinimumWidth(width CGFloat) { m.id.Send(Sel("setMinimumWidth:"), width) }

// AddItem appends an item.
func (m Menu) AddItem(it MenuItem) { m.id.Send(Sel("addItem:"), it.id) }

// AddSeparator appends a separator line.
func (m Menu) AddSeparator() { m.id.Send(Sel("addItem:"), cls("NSMenuItem", "separatorItem")) }

// AddTitled appends a plain item with the given title and returns it.
func (m Menu) AddTitled(title string) MenuItem {
	it := NewMenuItem(title)
	m.AddItem(it)
	return it
}

// RemoveAllItems clears the menu, for repopulating submenus on refresh.
func (m Menu) RemoveAllItems() { m.id.Send(Sel("removeAllItems")) }

// ApplyDarkAppearance sets the dark (NSDarkAqua) appearance on this menu and all
// its submenus, so the dropdown matches the app's dark panels rather than
// following the system light/vibrant menu style. Ports uiApplyMenuTreeDarkAppearance.
// The appearance sticks on the submenu objects, so items repopulated later on
// refresh inherit it.
func (m Menu) ApplyDarkAppearance() {
	m.id.Send(Sel("setAppearance:"), darkAquaAppearance())
	items := send(m.id, "itemArray")
	n := int(objc.Send[uint](items, Sel("count")))
	for i := 0; i < n; i++ {
		if sub := send(items.Send(Sel("objectAtIndex:"), uint(i)), "submenu"); sub != nilID {
			Menu{id: sub}.ApplyDarkAppearance()
		}
	}
}

// OnWillOpen registers fn to run each time the menu is about to open, so the
// status can be refreshed live as the user clicks the menu. Ports the
// NSMenuDelegate menuWillOpen: hook used by StatusBarController.
func (m Menu) OnWillOpen(fn func()) {
	menuOpenMu.Lock()
	menuOpenByID[m.id] = fn
	menuOpenMu.Unlock()
	m.id.Send(Sel("setDelegate:"), menuDelegate())
}

var (
	menuOpenMu    sync.Mutex
	menuOpenByID  = map[ID]func(){}
	menuDelegID   ID
	menuDelegOnce sync.Once
)

// menuDelegate returns the shared NSMenuDelegate, registering its class on first
// use; menuWillOpen: fires the registered closure for that menu.
func menuDelegate() ID {
	menuDelegOnce.Do(func() {
		_, err := registerClass("AKMenuDelegate", "NSObject", []method{{
			cmd: Sel("menuWillOpen:"),
			fn: func(self ID, _ SEL, menu ID) {
				menuOpenMu.Lock()
				fn := menuOpenByID[menu]
				menuOpenMu.Unlock()
				if fn != nil {
					fn()
				}
			},
		}})
		if err != nil {
			panic(err)
		}
		menuDelegID = cls("AKMenuDelegate", "alloc").Send(Sel("init"))
	})
	return menuDelegID
}

// NewMenuItem creates a plain, actionless NSMenuItem.
func NewMenuItem(title string) MenuItem {
	return newMenuItemKey(title, "")
}

// NewMenuItemKey creates a plain item with a key equivalent (e.g. "q" for Quit).
func NewMenuItemKey(title, key string) MenuItem {
	return newMenuItemKey(title, key)
}

// newMenuItemKey creates an NSMenuItem with a key equivalent (e.g. "q" for Quit).
func newMenuItemKey(title, key string) MenuItem {
	it := cls("NSMenuItem", "alloc").Send(Sel("initWithTitle:action:keyEquivalent:"),
		nsString(title), SEL(0), nsString(key))
	return MenuItem{id: it}
}

// menuItemSelector creates an item bound to a raw Objective-C selector with no
// explicit target, so the message travels the responder chain. Used for the
// standard editing commands (cut:/copy:/paste:/selectAll:) which the focused
// text field handles.
func menuItemSelector(title, selName, key string) MenuItem {
	it := cls("NSMenuItem", "alloc").Send(Sel("initWithTitle:action:keyEquivalent:"),
		nsString(title), Sel(selName), nsString(key))
	return MenuItem{id: it}
}

// SetSubmenu attaches a submenu.
func (it MenuItem) SetSubmenu(m Menu) { it.id.Send(Sel("setSubmenu:"), m.id) }

// SetEnabled toggles whether the item is selectable.
func (it MenuItem) SetEnabled(v bool) { it.id.Send(Sel("setEnabled:"), v) }

// SetHidden toggles item visibility.
func (it MenuItem) SetHidden(v bool) { it.id.Send(Sel("setHidden:"), v) }

// SetState sets the checkmark state (StateOn / StateOff).
func (it MenuItem) SetState(v int) { it.id.Send(Sel("setState:"), v) }

// SetTitle updates the plain title.
func (it MenuItem) SetTitle(s string) { it.id.Send(Sel("setTitle:"), nsString(s)) }

// SetAttributedTitle sets a styled title.
func (it MenuItem) SetAttributedTitle(a AttributedString) {
	it.id.Send(Sel("setAttributedTitle:"), a.id)
}

// SetMutedTitle styles the item as a muted, bold disabled header.
func (it MenuItem) SetMutedTitle(text string) {
	it.SetAttributedTitle(attributedMenuHeader(text))
}

// SetColoredTitle styles the item's title in a single color (status header).
func (it MenuItem) SetColoredTitle(text string, c Color) {
	it.SetAttributedTitle(attributedColored(text, c))
}

// SetSubtitle uses the system's secondary label color and smaller regular font.
func (it MenuItem) SetSubtitle(text string) {
	a := attributedColored(text, Color{id: cls("NSColor", "secondaryLabelColor")})
	a.id.Send(Sel("addAttribute:value:range:"), nsString(attrFont),
		cls("NSFont", "systemFontOfSize:", CGFloat(12)), Range{Length: attributedLength(a.id)})
	it.SetAttributedTitle(a)
}

// SetSymbol uses a native template image, which follows the menu's appearance.
func (it MenuItem) SetSymbol(name string) {
	sel := Sel("imageWithSystemSymbolName:accessibilityDescription:")
	if !objc.Send[bool](ID(Class("NSImage")), Sel("respondsToSelector:"), sel) {
		return
	}
	img := cls("NSImage", "imageWithSystemSymbolName:accessibilityDescription:", nsString(name), nilID)
	if img != nilID {
		img.Send(Sel("setTemplate:"), true)
		it.id.Send(Sel("setImage:"), img)
	}
}

// SetAction makes the item invoke fn when chosen. The closure captures whatever
// state the handler needs (e.g. a VPN name), so AppKit's representedObject/tag
// plumbing is replaced by ordinary Go closures. All items share one dispatcher
// object; the per-item tag selects the closure.
func (it MenuItem) SetAction(fn func()) { bindAction(it.id, fn) }

// bindAction wires any target/action responder (NSMenuItem, NSButton) to a Go
// closure via the shared dispatcher: a fresh tag stored on the object selects
// the closure in akInvoke:.
func bindAction(obj ID, fn func()) {
	tag := registerAction(fn)
	obj.Send(Sel("setTag:"), tag)
	obj.Send(Sel("setTarget:"), actionDispatcher())
	obj.Send(Sel("setAction:"), Sel("akInvoke:"))
}

// ---------------------------------------------------------------------------
// Go target/action dispatch.
// ---------------------------------------------------------------------------

var (
	actionMu       sync.Mutex
	actionByTag    = map[int]func(){}
	actionNextTag  = 1
	dispatcherID   ID
	dispatcherOnce sync.Once
)

// registerAction stores fn under a fresh tag and returns it.
func registerAction(fn func()) int {
	actionMu.Lock()
	defer actionMu.Unlock()
	tag := actionNextTag
	actionNextTag++
	actionByTag[tag] = fn
	return tag
}

// ResetActions clears the action registry. Call before fully rebuilding a menu
// so closures for discarded items do not accumulate. The dispatcher object and
// the tag counter persist (stale tags simply never fire again).
func ResetActions() {
	actionMu.Lock()
	defer actionMu.Unlock()
	actionByTag = map[int]func(){}
}

// actionDispatcher returns the shared dispatcher instance, registering its class
// on first use. The class answers two selectors, both invoked on the main
// thread by AppKit: akInvoke: looks up a clicked item's tag in the action
// registry and runs the matching Go closure; akRunMain: drains one closure from
// the main-thread queue (see RunOnMain).
func actionDispatcher() ID {
	dispatcherOnce.Do(func() {
		_, err := registerClass("AKActionDispatcher", "NSObject", []method{
			{
				cmd: Sel("akInvoke:"),
				fn: func(self ID, _ SEL, sender ID) {
					tag := objc.Send[int](sender, Sel("tag"))
					actionMu.Lock()
					fn := actionByTag[tag]
					actionMu.Unlock()
					if fn != nil {
						// Recover so a panic in a menu handler can never take the
						// whole menu-bar process down.
						defer recoverPanic()
						fn()
					}
				},
			},
			{
				cmd: Sel("akRunMain:"),
				fn:  func(self ID, _ SEL, arg ID) { drainOneMainFunc() },
			},
		})
		if err != nil {
			panic(err)
		}
		dispatcherID = cls("AKActionDispatcher", "alloc").Send(Sel("init"))
	})
	return dispatcherID
}
