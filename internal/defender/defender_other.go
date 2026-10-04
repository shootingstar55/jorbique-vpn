//go:build !windows

package defender

// AddExclusions is Windows-only: Defender, and its exclusion list, exist nowhere else.
func AddExclusions(_ string) {
	// Nothing to exclude: Defender does not exist on this platform.
}
