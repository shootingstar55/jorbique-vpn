//go:build darwin

package vpnmgr

// DaemonModeSupported is true on darwin: the SCDynamicStore/IOKit watchers and
// the CFRunLoop come from applekit (see daemon_purego.go).
func DaemonModeSupported() bool { return true }
