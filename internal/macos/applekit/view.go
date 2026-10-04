//go:build darwin

package applekit

// View wraps an NSView and exposes the handful of operations the panels need:
// layer-backed styling and frame placement. The panels use fixed-size windows,
// so manual frames replace the Auto Layout anchor graph ui.m built — visually
// equivalent for non-resizable dialogs and far less message traffic across the
// bridge.
type View struct{ id ID }

// contentView returns a window's content view.
func contentViewOf(window ID) View { return View{id: send(window, "contentView")} }

// Autoresizing mask options (NSAutoresizingMaskOptions): which margins/dimensions
// flex when the superview resizes. Combine with |. Effective only when the view
// keeps translatesAutoresizingMaskIntoConstraints = YES (see EnableFrameLayout).
const (
	ViewWidthSizable  uint = 2  // flexible width
	ViewHeightSizable uint = 16 // flexible height
)

// SetAutoresizingMask sets the springs-and-struts mask for a frame-laid view.
func (v View) SetAutoresizingMask(mask uint) { v.id.Send(Sel("setAutoresizingMask:"), mask) }

// EnableFrameLayout re-enables frame + autoresizing-mask layout on a view. The
// labelWithString:/wrappingLabelWithString:/textField factory constructors set
// translatesAutoresizingMaskIntoConstraints = NO (for Auto Layout); call this
// before SetFrame when positioning such a view by frame outside a stack.
func (v View) EnableFrameLayout() {
	v.id.Send(Sel("setTranslatesAutoresizingMaskIntoConstraints:"), true)
}

// addSubview adds a child view.
func (v View) addSubview(child ID) { v.id.Send(Sel("addSubview:"), child) }

// wantsLayer turns on layer backing so background/corner styling applies.
func (v View) wantsLayer() ID {
	v.id.Send(Sel("setWantsLayer:"), true)
	return send(v.id, "layer")
}

// SetBackground fills the view with color via its backing layer.
func (v View) SetBackground(c Color) {
	layer := v.wantsLayer()
	layer.Send(Sel("setBackgroundColor:"), c.cgColor())
}

// cgColor returns the CGColorRef for a Color, for CALayer styling.
func (c Color) cgColor() ID { return send(c.id, "CGColor") }

// primaryTextColor is the near-white primary text color (uiWinTextPrimary).
func primaryTextColor() Color { return srgb(248, 250, 252, 1) }
