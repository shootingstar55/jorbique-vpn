package network

import (
	"encoding/json"
	"os"
	"strings"

	"jorbiquevpn/internal/constants"
)

// GetTunInterfaceNameFromConfig reads interface_name from sing-box JSON
func GetTunInterfaceNameFromConfig(configPath string) string {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}
	var cfg struct {
		Inbounds []struct {
			InterfaceName string `json:"interface_name"`
		} `json:"inbounds"`
	}
	if json.Unmarshal(data, &cfg) != nil || len(cfg.Inbounds) == 0 {
		return ""
	}
	return strings.TrimSpace(cfg.Inbounds[0].InterfaceName)
}

// FindTunInterface finds the sing-box TUN adapter. It tries, in order:
//  1. the adapter that owns the TUN IP (e.g. 172.31.255.*) — the definitive "ready for routing"
//     signal, reliable on Win10 where a fresh wintun adapter lags in reporting Status 'Up';
//  2. the adapter with the configured interface name (any status) — covers the brief window
//     before the address is attached.
func FindTunInterface(interfaceName, ipRange string) (interfaceIndex int, interfaceAlias string, ok bool) {
	if ipRange == "" {
		ipRange = constants.TunIPRange
	}
	// Native read of the OS adapter table — no process spawn, instant, same on Win10/Win11.
	return findTunInterfaceNative(interfaceName, ipRange)
}

// ClearDnsCache flushes the Windows DNS resolver cache.
func ClearDnsCache() {
	flushDNSCacheNative()
}
