//go:build !windows

package network

func flushDNSCacheNative() {
	// This helper is Windows-only; other platforms manage DNS through their VPN backend.
}
