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
