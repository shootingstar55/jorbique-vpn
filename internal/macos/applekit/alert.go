//go:build darwin

package applekit

// Alert styles (NSAlertStyle).
const (
	AlertWarning       = 0 // NSAlertStyleWarning
	AlertInformational = 1 // NSAlertStyleInformational
	AlertCritical      = 2 // NSAlertStyleCritical
)

// PresentAlert shows a modal alert with a single OK button, styled to match the
// app: a dark appearance and the app's icon (set via SetAppIcon). It blocks
// until dismissed and must be called on the main thread. Ports
// uiPresentThemedAlert.
func PresentAlert(title, body string, style int) {
	a := cls("NSAlert", "alloc").Send(Sel("init"))
	a.Send(Sel("setMessageText:"), nsString(title))
	a.Send(Sel("setInformativeText:"), nsString(body))
	a.Send(Sel("setAlertStyle:"), style)
	a.Send(Sel("addButtonWithTitle:"), nsString("ОК"))
	if icon := appIconImage; icon != nilID {
		a.Send(Sel("setIcon:"), icon)
	}

	// Force the dark appearance on the alert's own window (accessing `window`
	// creates the panel). The previous approach — flipping NSApp.appearance for
	// the duration of the modal — left the alert's text black while the chrome
	// went dark on light-theme Macs (the labels had already resolved their
	// colors), i.e. unreadable black-on-gray. A window-level appearance is
	// resolved when the labels draw, so text and background always agree. It
	// also keeps NSApp.appearance untouched, so IsDarkMode keeps reporting the
	// real system theme for the menu colors while the modal runs.
	send(a, "window").Send(Sel("setAppearance:"), darkAquaAppearance())
	a.Send(Sel("runModal"))
}

// darkAquaAppearance returns the NSDarkAqua appearance.
func darkAquaAppearance() ID {
	return cls("NSAppearance", "appearanceNamed:", nsString("NSDarkAqua"))
}

// appIconImage is the loaded application icon, shared by alerts and the Dock.
var appIconImage ID

// SetAppIcon loads an icon from path (e.g. the bundled AppIcon.icns) and sets it
// as the application icon, so alerts and the Dock show the real icon instead of
// the generic placeholder. A missing/unreadable path is ignored.
func SetAppIcon(path string) {
	if path == "" {
		return
	}
	img := cls("NSImage", "alloc").Send(Sel("initWithContentsOfFile:"), nsString(path))
	if img == nilID {
		return
	}
	appIconImage = img
	cls("NSApplication", "sharedApplication").Send(Sel("setApplicationIconImage:"), img)
}
