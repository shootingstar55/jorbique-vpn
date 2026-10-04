//go:build !windows

package network

func findTunInterfaceNative(_, _ string) (int, string, bool) { return 0, "", false }
