package directbypass

import (
	"bytes"
	"jorbiquevpn/internal/sysexec"
)

// runReadCmd runs a read-only system command; callers pass fixed absolute
// binaries (/usr/bin/plutil) with app-derived paths, never user input.
func runReadCmd(name string, args ...string) string {
	cmd := sysexec.Command(name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return ""
	}
	return out.String()
}
