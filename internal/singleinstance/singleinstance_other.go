//go:build !windows

package singleinstance

// TryMutex is a no-op on non-Windows builds (Wails target is Windows).
func TryMutex() (release func(), ok bool) {
	return func() {
		// No mutex was taken, so there is nothing to release.
	}, true
}
