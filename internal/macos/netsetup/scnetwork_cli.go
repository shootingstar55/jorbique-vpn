//go:build !darwin || (darwin && !cgo)

package netsetup

import (
	"regexp"
	"strings"

	"jorbiquevpn/internal/macos/procutil"
)

var networkServiceOrderLine = regexp.MustCompile(`^\(\d+\)\s+(.+)$`)

func listEnabledNetworkServicesCLI() []string {
	st, out := procutil.RunAndCapture("/usr/sbin/networksetup", "-listnetworkserviceorder")
	if st != 0 {
		return nil
	}
	var services []string
	for _, line := range strings.Split(out, "\n") {
		s := strings.TrimSpace(line)
		if strings.HasPrefix(s, "(*)") {
			continue
		}
		m := networkServiceOrderLine.FindStringSubmatch(s)
		if len(m) == 2 && m[1] != "" {
			services = append(services, m[1])
		}
	}
	return services
}

func bsdNameForNetworkServiceCLI(service string) string {
	service = strings.TrimSpace(service)
	if service == "" {
		return ""
	}
	st, out := procutil.RunAndCapture("/usr/sbin/networksetup", "-listallhardwareports")
	if st != 0 {
		return ""
	}
	foundService := false
	for _, line := range strings.Split(out, "\n") {
		s := strings.TrimSpace(line)
		if strings.HasPrefix(s, "Hardware Port:") && strings.Contains(s, service) {
			foundService = true
			continue
		}
		if foundService && strings.HasPrefix(s, "Device:") {
			return strings.TrimSpace(strings.TrimPrefix(s, "Device:"))
		}
	}
	return ""
}
