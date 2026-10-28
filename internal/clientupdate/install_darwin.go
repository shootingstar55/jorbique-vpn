//go:build darwin

package clientupdate

import "jorbiquevpn/internal/sysexec"

func installPackage(path string, _ packageInfo) error {
	// Installer retains macOS's authentication and package installation flow.
	return sysexec.Command("/usr/bin/open", "-b", "com.apple.installer", path).Run()
}
