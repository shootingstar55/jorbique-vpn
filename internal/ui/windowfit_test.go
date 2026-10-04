package ui

import "testing"

// The window opens at a size the screen may not have. A display at 125% turns
// 960 logical pixels into 1200 physical ones; the title bar then sits above the
// top edge of a 1080p screen, and a window whose title bar is off-screen cannot
// be moved, minimised or resized — which is how it was reported.
func TestFitWindowToScreen(t *testing.T) {
	tests := []struct {
		name         string
		w, h, sw, sh int
		wantW, wantH int
	}{
		{"fits with room to spare", 754, 960, 1920, 1200, 754, 960},
		{"taller than the screen", 754, 960, 1536, 864, 754, 768},
		{"wider than the screen", 754, 960, 640, 1200, 600, 960},
		{"both", 754, 960, 640, 700, 600, 604},
		{"nothing reported", 754, 960, 0, 0, 754, 960},
		{"a screen smaller than its own chrome", 754, 960, 20, 40, 754, 960},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, h := FitWindowToScreen(tt.w, tt.h, tt.sw, tt.sh)
			if w != tt.wantW || h != tt.wantH {
				t.Errorf("FitWindowToScreen(%d,%d,%d,%d) = %d,%d, want %d,%d",
					tt.w, tt.h, tt.sw, tt.sh, w, h, tt.wantW, tt.wantH)
			}
		})
	}
}

// The page reports what it occupies and the window follows, but not for every
// pixel: a window that resizes itself while the user reads it is worse than a
// few pixels of slack. And the screen is still the ceiling — content taller
// than the desktop must not push the title bar off it.
func TestWindowHeightForContent(t *testing.T) {
	const screenW, screenH = 1920, 1200

	if h, change := windowHeightForContent(631, 820, 754, screenW, screenH); !change || h != 671 {
		t.Errorf("content 631 in an 820 window = %d,%v, want 671,true", h, change)
	}
	if _, change := windowHeightForContent(631, 671, 754, screenW, screenH); change {
		t.Error("a window already the right height was resized again")
	}
	if _, change := windowHeightForContent(636, 671, 754, screenW, screenH); change {
		t.Error("five pixels moved the window")
	}
	if h, change := windowHeightForContent(660, 671, 754, screenW, screenH); !change || h != 700 {
		t.Errorf("content grown past the threshold = %d,%v, want 700,true", h, change)
	}
	// A screen 864 logical pixels tall is a 1080p display at 125%.
	if h, change := windowHeightForContent(900, 820, 754, 1536, 864); !change || h != 768 {
		t.Errorf("content taller than the screen = %d,%v, want 768,true", h, change)
	}
	if _, change := windowHeightForContent(0, 820, 754, screenW, screenH); change {
		t.Error("a page that reported nothing resized the window")
	}
}
