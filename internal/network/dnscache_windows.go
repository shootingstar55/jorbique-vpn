//go:build windows

package network

import "golang.org/x/sys/windows"

var (
	moddnsapi                 = windows.NewLazySystemDLL("dnsapi.dll")
	procDnsFlushResolverCache = moddnsapi.NewProc("DnsFlushResolverCache")
)

// flushDNSCacheNative flushes the system DNS resolver cache (equivalent of ipconfig /flushdns),
// without the powershell.exe spawn the connect path used to pay for it.
func flushDNSCacheNative() {
	_, _, _ = procDnsFlushResolverCache.Call()
}
