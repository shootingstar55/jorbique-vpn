//go:build windows

package network

import (
	"strings"
	"time"

	"jorbiquevpn/internal/constants"
	"jorbiquevpn/internal/utils"
)

const defaultSingboxTunName = "singtun"

// PrepareSingtunForConnect kills a sing-box left over from a crash or a hard disconnect, so the
// Wintun adapter it still holds is released before the next Connect. The adapter itself is no
// longer removed: each connect picks a fresh interface name, so a leftover can never collide
// with the new run, and forcing a device removal needed a PowerShell Remove-NetAdapter for a
// case that no longer blocks anything.
func PrepareSingtunForConnect(tunName string, logFn func(string, string)) {
	tunName = normalizeSingtunName(tunName)
	// Happy path is checked natively first (instant, no process spawn): if no leftover adapter is
	// present, skip the process kill entirely so a clean connect pays nothing here.
	if !singtunAdapterExists(tunName) {
		return
	}
	stopLingeringSingboxProcesses()
	if !singtunAdapterExists(tunName) {
		return // the lingering sing-box owned it; killing the process released it
	}
	if logFn != nil {
		logFn("TUN adapter "+tunName+" is still present; the next connect uses a fresh name", "INFO")
	}
}

func normalizeSingtunName(tunName string) string {
	tunName = strings.TrimSpace(tunName)
	if tunName == "" {
		return defaultSingboxTunName
	}
	return tunName
}

// stopLingeringSingboxProcesses force-kills any stray sing-box, releasing the Wintun adapter
// it held.
func stopLingeringSingboxProcesses() {
	_ = utils.TerminateByName(constants.ProcessExeName)
	time.Sleep(200 * time.Millisecond) // let the driver tear the adapter down
}

func singtunAdapterExists(tunName string) bool {
	// Native enumeration sees the adapter (any status) reliably and instantly; the PowerShell
	// Get-NetAdapter could miss a just-orphaned adapter and was slow to spawn.
	return adapterExistsByNameNative(tunName)
}
