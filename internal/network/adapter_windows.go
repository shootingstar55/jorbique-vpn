//go:build windows

package network

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Native adapter discovery via GetAdaptersAddresses. This replaces the per-call powershell.exe
// spawns used for TUN detection: each PowerShell launch costs hundreds of ms (worse on Win11 with
// Defender), and the connect path polls detection repeatedly. Reading the OS adapter table
// directly is effectively instant and identical on Win10/Win11.

// tunIPMatchPrefix turns an IP glob like "100.64.0.*" into the literal prefix "100.64.0.".
func tunIPMatchPrefix(ipRange string) string {
	ipRange = strings.TrimSpace(ipRange)
	if i := strings.IndexByte(ipRange, '*'); i >= 0 {
		return ipRange[:i]
	}
	return ipRange
}

func ipv4Adapters() *windows.IpAdapterAddresses {
	// Default enumeration (operational adapters). INCLUDE_ALL_INTERFACES is deliberately NOT set:
	// it surfaced non-present "ghost" device nodes left by prior Wintun usage that Remove-NetAdapter
	// cannot delete, causing the cleanup to spin for 10-25s reporting "still present". The collision
	// problem is solved at the source instead — each connect uses a unique TUN interface name.
	// INCLUDE_GATEWAYS populates FirstGatewayAddress for the recovery path.
	const flags = windows.GAA_FLAG_SKIP_ANYCAST | windows.GAA_FLAG_SKIP_MULTICAST |
		windows.GAA_FLAG_INCLUDE_GATEWAYS
	var size uint32
	_ = windows.GetAdaptersAddresses(windows.AF_UNSPEC, flags, 0, nil, &size)
	if size == 0 {
		return nil
	}
	for attempts := 0; attempts < 3; attempts++ {
		buf := make([]byte, size)
		aa := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, flags, 0, aa, &size)
		if err == nil {
			return aa
		}
		if err != windows.ERROR_BUFFER_OVERFLOW {
			return nil
		}
		// size now holds the required length; loop retries with the larger buffer.
	}
	return nil
}

// findTunInterfaceNative locates the sing-box TUN adapter without spawning a process:
//  1. an adapter with a unicast IPv4 in ipRange (e.g. 100.64.0.*) — the definitive routing-ready
//     signal, independent of adapter Status (which lags on Win10);
//  2. an adapter whose friendly name equals interfaceName (any status) — covers the brief window
//     before the address is attached.
func findTunInterfaceNative(interfaceName, ipRange string) (ifIndex int, alias string, ok bool) {
	prefix := tunIPMatchPrefix(ipRange)
	aa := ipv4Adapters()
	if aa == nil {
		return 0, "", false
	}
	var nameIdx int
	var nameAlias string
	for cur := aa; cur != nil; cur = cur.Next {
		friendly := windows.UTF16PtrToString(cur.FriendlyName)
		if adapterHasIPv4Prefix(cur, prefix) {
			return int(cur.IfIndex), friendly, true
		}
		// Name-tier fallback: only an operational adapter counts as "ready". A down orphan also
		// carries the name "singtun"; without the status guard it would falsely report readiness
		// (the IP tier above is the real, status-independent signal).
		if interfaceName != "" && nameIdx == 0 && cur.OperStatus == windows.IfOperStatusUp &&
			strings.EqualFold(friendly, interfaceName) {
			nameIdx = int(cur.IfIndex)
			nameAlias = friendly
		}
	}
	if nameIdx != 0 {
		return nameIdx, nameAlias, true
	}
	return 0, "", false
}

// adapterExistsByNameNative reports whether an adapter with this friendly name is present (any
// status). Used to detect a leftover singtun adapter before connect.
func adapterExistsByNameNative(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for cur := ipv4Adapters(); cur != nil; cur = cur.Next {
		if strings.EqualFold(windows.UTF16PtrToString(cur.FriendlyName), name) {
			return true
		}
	}
	return false
}

func adapterHasIPv4Prefix(adapter *windows.IpAdapterAddresses, prefix string) bool {
	if prefix == "" {
		return false
	}
	for ua := adapter.FirstUnicastAddress; ua != nil; ua = ua.Next {
		if ip4 := ua.Address.IP().To4(); ip4 != nil && strings.HasPrefix(ip4.String(), prefix) {
			return true
		}
	}
	return false
}
