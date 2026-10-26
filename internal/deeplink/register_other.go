//go:build !windows

package deeplink

// Register is Windows-only: macOS reads the scheme from the app bundle's
// Info.plist and Android from its manifest.
func Register(string) error { return nil }
