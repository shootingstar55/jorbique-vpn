//go:build darwin

package applekit

import "github.com/ebitengine/purego/objc"

const (
	addAttributeSelector   = "addAttribute:value:range:"
	initWithStringSelector = "initWithString:"
)

// Objective-C attributed-string attribute names. Like the appearance constants,
// these are exported NSString globals whose literal values are stable and
// documented; using the literals avoids dlsym'ing the globals.
const (
	attrFont            = "NSFont"
	attrForegroundColor = "NSColor"
	attrBaselineOffset  = "NSBaselineOffset"
)

// nsFontWeightMedium is the documented value of NSFontWeightMedium.
const nsFontWeightMedium CGFloat = 0.23

// AttributedString wraps an NSAttributedString built by this package.
type AttributedString struct{ id ID }

// attributedStatusTitle builds the status-bar button's title: a monospaced-digit
// system font at size 23, the given color, nudged down 4pt to sit correctly in
// the menu bar. Ports uiAttributedTitle.
func attributedStatusTitle(text string, color Color) AttributedString {
	a := cls("NSMutableAttributedString", "alloc").Send(Sel(initWithStringSelector), nsString(text))
	font := cls("NSFont", "monospacedDigitSystemFontOfSize:weight:", CGFloat(23), nsFontWeightMedium)
	r := Range{Location: 0, Length: attributedLength(a)}
	a.Send(Sel(addAttributeSelector), nsString(attrFont), font, r)
	a.Send(Sel(addAttributeSelector), nsString(attrForegroundColor), color.id, r)
	a.Send(Sel(addAttributeSelector), nsString(attrBaselineOffset), nsNumberDouble(-4), r)
	return AttributedString{id: a}
}

// attributedMenuHeader builds the disabled menu header title in a muted color and
// a small bold system font, approximating ui.m's uiMenuTitleAttrs.
func attributedMenuHeader(text string) AttributedString {
	a := cls("NSMutableAttributedString", "alloc").Send(Sel(initWithStringSelector), nsString(text))
	font := cls("NSFont", "boldSystemFontOfSize:", CGFloat(12))
	r := Range{Location: 0, Length: attributedLength(a)}
	a.Send(Sel(addAttributeSelector), nsString(attrFont), font, r)
	a.Send(Sel(addAttributeSelector), nsString(attrForegroundColor), menuMutedTextColor().id, r)
	return AttributedString{id: a}
}

// attributedColored builds a plain string in a single foreground color, used
// for the menu's status header line (green/red/yellow per connection state).
func attributedColored(text string, color Color) AttributedString {
	a := cls("NSMutableAttributedString", "alloc").Send(Sel(initWithStringSelector), nsString(text))
	r := Range{Location: 0, Length: attributedLength(a)}
	a.Send(Sel(addAttributeSelector), nsString(attrForegroundColor), color.id, r)
	return AttributedString{id: a}
}

// attributedLength returns the character length of an NSAttributedString.
func attributedLength(a ID) uint {
	return objc.Send[uint](a, Sel("length"))
}

// nsNumberDouble boxes a float64 in an NSNumber.
func nsNumberDouble(v CGFloat) ID {
	return cls("NSNumber", "numberWithDouble:", v)
}
