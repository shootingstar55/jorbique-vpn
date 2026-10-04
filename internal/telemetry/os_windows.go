package telemetry

import (
	"context"
	"fmt"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func computerName(context.Context) string {
	// msinfo32's System Name is the computer name, not the DNS hostname.
	name, _ := windows.ComputerName()
	return name
}

func operatingSystem() (name, version, build string) {
	const verNTWorkstation = 1
	v := windows.RtlGetVersion()
	name = windowsName(v.MajorVersion, v.MinorVersion, v.BuildNumber, v.ProductType == verNTWorkstation)
	version = fmt.Sprintf("%d.%d", v.MajorVersion, v.MinorVersion)
	build = fmt.Sprint(v.BuildNumber)
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return
	}
	defer key.Close()
	if release, _, err := key.GetStringValue("DisplayVersion"); err == nil && release != "" {
		version = release
	}
	if revision, _, err := key.GetIntegerValue("UBR"); err == nil {
		build = fmt.Sprintf("%d.%d", v.BuildNumber, revision)
	}
	return
}
