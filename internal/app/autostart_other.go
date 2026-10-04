//go:build !windows

package app

import "errors"

// Logon autostart is a Windows-only capability in this GUI (macOS ships the native menu-bar app
// with launchd autostart instead). Report it honestly so the UI does not show a fake "enabled".
func (a *App) IsAutostartEnabled() bool {
	return false
}

// IsAutostartSupported lets the UI hide the autostart toggle on platforms where SetAutostartEnabled
// can only ever fail, so users never get an error toast for a control that cannot work here.
func (a *App) IsAutostartSupported() bool {
	return false
}

func (a *App) SetAutostartEnabled(_ bool) error {
	return errors.New("autostart is not supported on this platform")
}
