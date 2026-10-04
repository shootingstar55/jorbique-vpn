//go:build darwin

package applekit

// Color wraps an NSColor. NSColor instances returned here are autoreleased; they
// are used immediately to build attributed strings (which retain them), so no
// explicit lifetime management is needed at the call sites in Phase 2.
type Color struct{ id ID }

// systemColor returns one of NSColor's named system colors (e.g. "systemGreen").
func systemColor(name string) Color {
	return Color{id: cls("NSColor", name)}
}

// rgba builds an sRGB NSColor. Components are 0..1.
func rgba(r, g, b, a CGFloat) Color {
	return Color{id: cls("NSColor", "colorWithRed:green:blue:alpha:", r, g, b, a)}
}

// OKColor / StopColor / YellowColor are the status-glyph colors, matching ui.m's
// uiOkColor / uiStopColor / uiYellowColor: vivid system colors in dark mode, and
// hand-tuned darker variants in light mode for contrast against the light menu
// bar.
func OKColor() Color {
	if IsDarkMode() {
		return systemColor("systemGreenColor")
	}
	return rgba(0.0, 0.5, 0.0, 1)
}

func StopColor() Color {
	if IsDarkMode() {
		return systemColor("systemRedColor")
	}
	return rgba(0.75, 0.1, 0.1, 1)
}

func YellowColor() Color {
	if IsDarkMode() {
		return systemColor("systemYellowColor")
	}
	return rgba(0.7, 0.5, 0.0, 1)
}

// mutedTextColor is the slate-400 muted color used for secondary text inside
// the always-dark panels, matching ui.m's uiWinTextMuted. Panels force
// NSDarkAqua, so this stays fixed regardless of the system theme.
func mutedTextColor() Color { return srgb(148, 163, 184, 1) }

// menuMutedTextColor is the muted color for the disabled menu header. Unlike
// the panels, the status-bar menu follows the system theme (it cannot be
// forced dark), so slate-400 — fine on the dark menu — washes out on the light
// one; use a darker slate there.
func menuMutedTextColor() Color {
	if IsDarkMode() {
		return mutedTextColor()
	}
	return srgb(71, 85, 105, 1) // slate-600
}

// StatusColor maps a status keyword ("ok"/"yellow"/anything-else) to its color,
// mirroring ui_set_status_title's switch (default is the stop color).
func StatusColor(keyword string) Color {
	switch keyword {
	case "ok":
		return OKColor()
	case "yellow":
		return YellowColor()
	default:
		return StopColor()
	}
}

// srgb builds a colour from 0-255 components, which is how the Windows
// stylesheet the windows follow writes them.
func srgb(r, g, b, a CGFloat) Color {
	return Color{id: cls("NSColor", "colorWithSRGBRed:green:blue:alpha:", r/255, g/255, b/255, a)}
}
