//go:build !windows

package tray

// AutostartEnabled reports whether the app is scheduled to start at logon.
func AutostartEnabled() bool {
	return false
}

// SetAutostart enables or disables logon autostart.
func SetAutostart(_ bool) error {
	return nil
}

func isAutostartEnabled() bool {
	return false
}

// RefreshAutostartIfEnabled is a no-op off Windows.
func RefreshAutostartIfEnabled() error {
	return nil
}
