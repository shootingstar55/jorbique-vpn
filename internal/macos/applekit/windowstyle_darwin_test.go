//go:build darwin

package applekit

import "testing"

// The mask decides which title-bar buttons a window has, and making one
// resizable rewrites it wholesale — the place a button quietly goes missing.
//
// Checked as arithmetic rather than by building a window: AppKit traps when a
// window is created off the main thread, which a Go test never runs on.
func TestPanelMasksKeepEveryButton(t *testing.T) {
	for name, mask := range map[string]int{
		"panel":           panelStyleMask,
		"resizable panel": resizablePanelStyleMask,
	} {
		for button, bit := range map[string]int{
			"titled":      styleTitled,
			"closable":    styleClosable,
			"miniaturise": styleMiniaturize,
		} {
			if mask&bit == 0 {
				t.Errorf("the %s mask has no %s button (%b)", name, button, mask)
			}
		}
	}
	if resizablePanelStyleMask&styleResizable == 0 {
		t.Error("the resizable mask is not resizable")
	}
}
