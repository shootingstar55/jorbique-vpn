package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// The menu-bar icon is the app icon's face — one eye open, one winking, the
// tongue out — drawn small enough to sit among the other status items. It used
// to be a red "○", which read as an error and said nothing about whose icon it
// was.
//
// It is drawn here rather than shipped as image files so the state colors live
// in one place and the drawing is checked by tests on any platform. The canvas
// is 36px for an 18pt item, the 2x a Retina menu bar draws; AppKit scales it
// down on a 1x screen.
//
// Disconnected is a template image: plain black that macOS recolors to match
// the menu bar, like every other monochrome icon there. The other states fill
// the face with their color, so a glance says whether the tunnel is up.

const barIconPx = 36

// BarIconPoints is the size, in points, AppKit is told to draw the icon at.
const BarIconPoints = 18

// barIconStyle picks how the face is filled.
type barIconStyle int

const (
	barIconTemplate barIconStyle = iota // monochrome, recolored by macOS
	barIconOK                           // connected
	barIconYellow                       // connecting, restarting, or needs attention
)

// barIconStyleFor maps a status color keyword ("ok"/"yellow"/"stop") to a
// style. "stop" is the idle state, and idle is the template face: off is the
// normal state of a VPN client, not an alarm.
func barIconStyleFor(keyword string) barIconStyle {
	switch keyword {
	case "ok":
		return barIconOK
	case "yellow":
		return barIconYellow
	default:
		return barIconTemplate
	}
}

// Face colors per style, with the darker shade used for the features and the
// outline. The dark shade reads on both the light and the dark menu bar.
var barIconPalette = map[barIconStyle]struct{ fill, ink, tongue color.NRGBA }{
	barIconOK:     {color.NRGBA{0x3d, 0xd6, 0x8c, 0xff}, color.NRGBA{0x0b, 0x3b, 0x2a, 0xff}, color.NRGBA{0xa8, 0xf0, 0xc8, 0xff}},
	barIconYellow: {color.NRGBA{0xf5, 0xc5, 0x2b, 0xff}, color.NRGBA{0x4a, 0x35, 0x05, 0xff}, color.NRGBA{0xfb, 0xe7, 0x9a, 0xff}},
}

// shape reports whether the point (x, y), in canvas pixels, is inside it.
type shape func(x, y float64) bool

func disk(cx, cy, r float64) shape {
	return func(x, y float64) bool { return math.Hypot(x-cx, y-cy) <= r }
}

func ring(cx, cy, r, w float64) shape {
	return func(x, y float64) bool { return math.Abs(math.Hypot(x-cx, y-cy)-r) <= w/2 }
}

// arc is a stroke along a circle from angle a0 to a1 (degrees, counterclockwise
// from 3 o'clock, up is positive), with round caps.
func arc(cx, cy, r, w, a0, a1 float64) shape {
	rad := func(d float64) float64 { return d * math.Pi / 180 }
	cap0 := disk(cx+r*math.Cos(rad(a0)), cy-r*math.Sin(rad(a0)), w/2)
	cap1 := disk(cx+r*math.Cos(rad(a1)), cy-r*math.Sin(rad(a1)), w/2)
	band := ring(cx, cy, r, w)
	return func(x, y float64) bool {
		if cap0(x, y) || cap1(x, y) {
			return true
		}
		if !band(x, y) {
			return false
		}
		a := math.Atan2(-(y-cy), x-cx) * 180 / math.Pi
		for a < a0 {
			a += 360
		}
		return a <= a1
	}
}

// segment is a capsule: every point within r of the line from (x0,y0) to (x1,y1).
func segment(x0, y0, x1, y1, r float64) shape {
	dx, dy := x1-x0, y1-y0
	l2 := dx*dx + dy*dy
	return func(x, y float64) bool {
		t := ((x-x0)*dx + (y-y0)*dy) / l2
		t = math.Max(0, math.Min(1, t))
		return math.Hypot(x-(x0+t*dx), y-(y0+t*dy)) <= r
	}
}

func union(s ...shape) shape {
	return func(x, y float64) bool {
		for _, f := range s {
			if f(x, y) {
				return true
			}
		}
		return false
	}
}

// The face, in the 36px canvas. The tongue hangs past the chin, through the
// outline, which is what makes it read as a tongue at this size.
var (
	faceHead    = disk(18, 17, 15)
	faceOutline = ring(18, 17, 14.1, 2.6)
	faceEyes    = union(
		disk(12.6, 13.2, 2.4),              // the open eye
		arc(23.4, 14.6, 2.9, 2.3, 25, 155), // the wink
		arc(18, 17.2, 7.6, 2.3, 205, 335),  // the smile
	)
	faceTongue = segment(19.2, 25, 20.8, 28.6, 3.7)
	// faceTongueGap separates the tongue from the smile and the outline it
	// crosses; without it the three merge into one blot.
	faceTongueGap = segment(19.2, 25, 20.8, 28.6, 5.0)
	faceGroove    = segment(19.1, 22.5, 19.9, 26.9, 0.6)
)

// layer is one paint step: where, and the color (nil erases to transparent).
type layer struct {
	where shape
	paint *color.NRGBA
}

func barIconLayers(style barIconStyle) []layer {
	if style == barIconTemplate {
		black := color.NRGBA{0, 0, 0, 0xff}
		return []layer{
			{faceOutline, &black},
			{faceEyes, &black},
			{faceTongueGap, nil},
			{faceTongue, &black},
			{faceGroove, nil},
		}
	}
	p := barIconPalette[style]
	return []layer{
		{faceHead, &p.fill},
		{faceOutline, &p.ink},
		{faceEyes, &p.ink},
		{faceTongueGap, &p.ink},
		{faceTongue, &p.tongue},
		{faceGroove, &p.ink},
	}
}

// renderBarIcon draws the face at px×px with 4×4 supersampling for smooth
// edges.
func renderBarIcon(style barIconStyle, px int) *image.NRGBA {
	const ss = 4
	img := image.NewNRGBA(image.Rect(0, 0, px, px))
	layers := barIconLayers(style)
	scale := float64(barIconPx) / float64(px)
	for py := 0; py < px; py++ {
		for pxx := 0; pxx < px; pxx++ {
			var r, g, b, a float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x := (float64(pxx) + (float64(sx)+0.5)/ss) * scale
					y := (float64(py) + (float64(sy)+0.5)/ss) * scale
					var c *color.NRGBA
					for _, l := range layers {
						if l.where(x, y) {
							c = l.paint
						}
					}
					if c != nil {
						r += float64(c.R)
						g += float64(c.G)
						b += float64(c.B)
						a++
					}
				}
			}
			if a == 0 {
				continue
			}
			img.SetNRGBA(pxx, py, color.NRGBA{
				R: uint8(r/a + 0.5),
				G: uint8(g/a + 0.5),
				B: uint8(b/a + 0.5),
				A: uint8(a/(ss*ss)*255 + 0.5),
			})
		}
	}
	return img
}

// barIconKeyword is the icon color for a steady state. A connection that
// needs attention keeps its red header but shows the yellow face: the
// template face is the idle one, and a failing tunnel is not idle.
func barIconKeyword(p statusPresentation) string {
	if p.glyph == "⚠" {
		return "yellow"
	}
	return p.color
}

// barIconPNG returns the face for a status color keyword as PNG bytes, and
// whether macOS should treat it as a template image.
func barIconPNG(keyword string) (data []byte, template bool) {
	style := barIconStyleFor(keyword)
	var buf bytes.Buffer
	// Encoding an in-memory NRGBA cannot fail.
	_ = png.Encode(&buf, renderBarIcon(style, barIconPx))
	return buf.Bytes(), style == barIconTemplate
}
