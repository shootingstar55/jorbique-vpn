//go:build darwin

package applekit

// nsVariableStatusItemLength sizes a status item to fit its content.
const nsVariableStatusItemLength CGFloat = -1

// StatusItem wraps an NSStatusItem in the system menu bar.
type StatusItem struct{ id ID }

// InstallStatusItem creates a variable-length status item in the system status
// bar. The returned value must be kept referenced for the lifetime of the app
// (the status bar holds it, but losing the Go handle loses the ability to update
// it). Ports the NSStatusItem creation in ui.m's installStatusItemAndMenu.
func InstallStatusItem() StatusItem {
	bar := cls("NSStatusBar", "systemStatusBar")
	return StatusItem{id: bar.Send(Sel("statusItemWithLength:"), nsVariableStatusItemLength)}
}

// SetTitle sets the button's styled status title (the colored glyph). Ports
// ui_set_status_title's assignment to button.attributedTitle.
func (s StatusItem) SetTitle(text string, color Color) {
	button := send(s.id, "button")
	if button == nilID {
		return
	}
	button.Send(Sel("setAttributedTitle:"), attributedStatusTitle(text, color).id)
}

// SetMenu attaches the dropdown menu shown when the item is clicked.
func (s StatusItem) SetMenu(m Menu) { s.id.Send(Sel("setMenu:"), m.id) }

// Image wraps an NSImage built once and kept for the life of the app.
type Image struct{ id ID }

// NewImage decodes PNG data into an NSImage drawn at width×height points.
// template marks it as a template image, which macOS recolors to match the
// menu bar (black on a light bar, white on a dark one). The image is retained
// (alloc/init) and never released: callers build a handful and reuse them.
func NewImage(png []byte, width, height CGFloat, template bool, description string) Image {
	if len(png) == 0 {
		return Image{}
	}
	data := cls("NSData", "dataWithBytes:length:", &png[0], uint(len(png)))
	img := cls("NSImage", "alloc").Send(Sel("initWithData:"), data)
	if img == nilID {
		return Image{}
	}
	img.Send(Sel("setSize:"), Size{Width: width, Height: height})
	img.Send(Sel("setTemplate:"), template)
	if description != "" {
		img.Send(Sel("setAccessibilityDescription:"), nsString(description))
	}
	return Image{id: img}
}

// SetImage shows img in the status item in place of any title, at the given
// opacity (1 is opaque; lower values fade it, which the progress pulse uses).
func (s StatusItem) SetImage(img Image, alpha CGFloat) {
	button := send(s.id, "button")
	if button == nilID || img.id == nilID {
		return
	}
	button.Send(Sel("setTitle:"), nsString(""))
	button.Send(Sel("setImage:"), img.id)
	button.Send(Sel("setAlphaValue:"), alpha)
}

// SetToolTip sets the text shown when the pointer rests on the item.
func (s StatusItem) SetToolTip(text string) {
	if button := send(s.id, "button"); button != nilID {
		button.Send(Sel("setToolTip:"), nsString(text))
	}
}
