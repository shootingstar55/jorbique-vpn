package ui

// Window chrome and the taskbar are not ours to draw, but they take room out of
// the screen all the same: a title bar and a border at the top, the taskbar at
// the bottom. What is left over is what a window can actually occupy.
const (
	screenChromeHeight = 96
	screenChromeWidth  = 40
)

// FitWindowToScreen shrinks a window that would not fit the screen it opens on.
//
// The size the app asks for is in logical pixels and so is the screen size Wails
// reports, which is what makes the comparison meaningful at any DPI. On a
// display scaled to 125% the window used to open taller than the screen: its
// title bar sat above the top edge, where it cannot be grabbed, minimised or
// resized, and the bottom of it went under the taskbar.
//
// A screen size of zero means nothing was reported — one is left alone rather
// than resized to nothing.
func FitWindowToScreen(width, height, screenWidth, screenHeight int) (int, int) {
	if screenWidth > screenChromeWidth {
		if max := screenWidth - screenChromeWidth; width > max {
			width = max
		}
	}
	if screenHeight > screenChromeHeight {
		if max := screenHeight - screenChromeHeight; height > max {
			height = max
		}
	}
	return width, height
}
