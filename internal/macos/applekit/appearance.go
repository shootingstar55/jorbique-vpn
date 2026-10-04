//go:build darwin

package applekit

// IsDarkMode reports whether the app is currently rendering in a dark
// appearance. Ports the `bestMatchFromAppearancesWithNames:` check used by
// ui.m's color helpers. The appearance-name constants (NSAppearanceNameDarkAqua
// / NSAppearanceNameAqua) are exported as NSString globals whose values are the
// literals "NSDarkAqua" / "NSAqua"; constructing those strings directly avoids
// dlsym'ing the globals (and the integer-to-pointer conversion that would
// entail) while remaining faithful to -bestMatchFromAppearancesWithNames:.
func IsDarkMode() bool {
	app := cls("NSApplication", "sharedApplication")
	if app == nilID {
		return false
	}
	appearance := send(app, "effectiveAppearance")
	if appearance == nilID {
		return false
	}
	dark := nsString("NSDarkAqua")
	names := nsArray(dark, nsString("NSAqua"))
	best := send(appearance, "bestMatchFromAppearancesWithNames:", names)
	if best == nilID {
		return false
	}
	return goStringFromNS(best) == "NSDarkAqua"
}
