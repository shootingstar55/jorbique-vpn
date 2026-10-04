package telemetry

import (
	"context"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"jorbiquevpn/internal/sysexec"
)

func computerName(ctx context.Context) string {
	// ComputerName is the Name in System Settings, unlike the DNS hostname.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	name, err := sysexec.CommandContext(ctx, "/usr/sbin/scutil", "--get", "ComputerName").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(name))
}

func operatingSystem() (name, version, build string) {
	version, _ = unix.Sysctl("kern.osproductversion")
	build, _ = unix.Sysctl("kern.osversion")
	return "macOS", version, build
}
