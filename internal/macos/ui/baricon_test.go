package ui

import (
	"bytes"
	"image/png"
	"testing"
)

func TestBarIconStates(t *testing.T) {
	cases := []struct {
		keyword  string
		template bool
	}{
		{"stop", true},
		{"ok", false},
		{"yellow", false},
		{"", true},
	}
	for _, tc := range cases {
		data, template := barIconPNG(tc.keyword)
		if template != tc.template {
			t.Errorf("%q: template = %v, want %v", tc.keyword, template, tc.template)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("%q: not a PNG: %v", tc.keyword, err)
		}
		if b := img.Bounds(); b.Dx() != barIconPx || b.Dy() != barIconPx {
			t.Errorf("%q: %v, want %dpx square", tc.keyword, b, barIconPx)
		}
	}
}

// A template image is pure black and alpha: any color in it would be thrown
// away by macOS, so color there means the drawing went wrong.
func TestBarIconTemplateIsBlackAndAlpha(t *testing.T) {
	img := renderBarIcon(barIconTemplate, barIconPx)
	painted := 0
	for i := 0; i < len(img.Pix); i += 4 {
		r, g, b, a := img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]
		if a == 0 {
			continue
		}
		painted++
		if r != 0 || g != 0 || b != 0 {
			t.Fatalf("colored pixel %d,%d,%d in the template icon", r, g, b)
		}
	}
	// The face covers a good part of the canvas but is mostly line work.
	if total := barIconPx * barIconPx; painted < total/6 || painted > total*2/3 {
		t.Errorf("%d of %d pixels painted", painted, total)
	}
}

// The corners stay empty: the face is round and must not be clipped square.
func TestBarIconCornersAreClear(t *testing.T) {
	for _, s := range []barIconStyle{barIconTemplate, barIconOK, barIconYellow} {
		img := renderBarIcon(s, barIconPx)
		for _, p := range [][2]int{{0, 0}, {barIconPx - 1, 0}, {0, barIconPx - 1}, {barIconPx - 1, barIconPx - 1}} {
			if a := img.NRGBAAt(p[0], p[1]).A; a != 0 {
				t.Errorf("style %d: corner %v alpha %d", s, p, a)
			}
		}
	}
}

func TestBarIconKeyword(t *testing.T) {
	if got := barIconKeyword(computeStatusPresentation(nil, nil)); got != "stop" {
		t.Errorf("idle = %q, want stop", got)
	}
	if got := barIconKeyword(computeStatusPresentation([]string{"a"}, nil)); got != "ok" {
		t.Errorf("connected = %q, want ok", got)
	}
	if got := barIconKeyword(computeStatusPresentation([]string{"a"}, []string{"a"})); got != "yellow" {
		t.Errorf("needs attention = %q, want yellow", got)
	}
}

func TestSpinnerAlphaCycles(t *testing.T) {
	n := len(spinnerAlphas)
	if spinnerAlpha(0) != spinnerAlpha(n) || spinnerAlpha(-1) != spinnerAlpha(n-1) {
		t.Error("spinnerAlpha does not cycle")
	}
	for i := 0; i < n; i++ {
		if a := spinnerAlpha(i); a <= 0 || a > 1 {
			t.Errorf("alpha %v out of range", a)
		}
	}
}
