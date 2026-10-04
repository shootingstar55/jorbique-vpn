//go:build !windows

package main

import "errors"

// The client is a Windows application; these stand in so the package still builds and its
// tests still run on the host doing the building. Nothing here has an equivalent elsewhere:
// there is no UAC to ask, and no window to put a message box in.

func isAdmin() bool { return false }

func showErrorBox(string) {
	// No message box outside Windows; the caller already logged the error.
}

func showAdminMessageBox() {
	// No UAC outside Windows, so there is nothing to tell the user about.
}

func relaunchElevated() error { return errors.New("elevation is Windows-only") }
