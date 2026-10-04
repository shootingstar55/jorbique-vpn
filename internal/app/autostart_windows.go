//go:build windows

package app

import (
	"jorbiquevpn/internal/tray"
)

func (a *App) IsAutostartEnabled() bool {
	return tray.AutostartEnabled()
}

// IsAutostartSupported reports that logon autostart is available on Windows so the UI shows the toggle.
func (a *App) IsAutostartSupported() bool {
	return true
}

func (a *App) SetAutostartEnabled(on bool) (err error) {
	return tray.SetAutostart(on)
}
