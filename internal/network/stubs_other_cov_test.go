//go:build !windows

package network

import "testing"

func TestNonWindowsStubsDoNothing(t *testing.T) {
	flushDNSCacheNative()
	PrepareSingtunForConnect("tun0", func(string, string) {
		t.Fatal("no adapter cleanup should be reported off Windows")
	})
}
