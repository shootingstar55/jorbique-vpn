package telemetry

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// device is what the app said about the machine, for a platform this package
// cannot ask itself. On Android the hostname is "localhost" on every phone and
// the OS version is not readable from Go, so the dashboard's Device and OS
// columns came out as "localhost" and "android unknown".
var device struct {
	sync.Mutex
	name, osName, osVersion, osBuild string
}

// SetDevice names the device and its OS. Call it before Start: the OS goes on
// the resource once, the name is read on every push. Empty values leave the
// platform's own answer.
func SetDevice(name, osName, osVersion, osBuild string) {
	device.Lock()
	defer device.Unlock()
	device.name = strings.TrimSpace(name)
	device.osName = strings.TrimSpace(osName)
	device.osVersion = strings.TrimSpace(osVersion)
	device.osBuild = strings.TrimSpace(osBuild)
}

func deviceName(ctx context.Context) string {
	device.Lock()
	name := device.name
	device.Unlock()
	if name != "" {
		return name
	}
	return computerName(ctx)
}

func deviceOS() (name, version, build string) {
	device.Lock()
	name, version, build = device.osName, device.osVersion, device.osBuild
	device.Unlock()
	if name != "" {
		return name, version, build
	}
	return operatingSystem()
}

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

// Device is the name and OS the metrics report for this machine: the
// computer name (on Android the phone's name from Settings) and the OS with
// its version. The account service binds devices by the same name.
func Device(ctx context.Context) (name, osName string) {
	n, version, _ := deviceOS()
	osName = n
	if version != "" && version != "unknown" {
		osName += " " + version
	}
	return deviceName(ctx), osName
}
