//go:build !windows

package network

// PrepareSingtunForConnect is Windows-only: leftover Wintun adapters exist nowhere else.
func PrepareSingtunForConnect(_ string, _ func(string, string)) {
	// Other platforms have no stale TUN adapter to clean up before connecting.
}
