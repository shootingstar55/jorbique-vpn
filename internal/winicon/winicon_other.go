//go:build !windows

package winicon

// ApplyTitleBarFromExeDir is Windows-only: the title-bar icon comes from the .exe resources there.
func ApplyTitleBarFromExeDir(string) {
	// Other platforms take the window icon from the app bundle.
}
