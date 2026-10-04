package telemetry

import (
	"fmt"
	"strings"
)

func knownVersion(value string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return "unknown"
}

// Windows 11 still reports NT 10.0; ProductName in the registry can still say
// Windows 10. Use the native build and product type, not that marketing string.
func windowsName(major, minor, build uint32, workstation bool) string {
	if !workstation {
		return "Windows Server"
	}
	switch {
	case major == 10 && build >= 22000:
		return "Windows 11"
	case major == 10:
		return "Windows 10"
	case major == 6 && minor == 3:
		return "Windows 8.1"
	case major == 6 && minor == 2:
		return "Windows 8"
	case major == 6 && minor == 1:
		return "Windows 7"
	default:
		return fmt.Sprintf("Windows %d.%d", major, minor)
	}
}
