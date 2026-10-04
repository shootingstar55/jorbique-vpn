//go:build !darwin

package vpnmgr

func DaemonModeSupported() bool { return false }
