//go:build !windows

package tray

// HideConsole is a no-op on non-Windows
func HideConsole() {
	// Only Windows opens a console window next to the tray app.
}
