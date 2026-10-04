//go:build windows

package core

import (
	"strings"
	"unsafe"

	"jorbiquevpn/internal/constants"

	"golang.org/x/sys/windows"
)

const stillActive = 259 // STILL_ACTIVE — process has not exited

// singboxProcessExistsByImageName reports any running sing-box.exe (fallback when PID file is missing).
func singboxProcessExistsByImageName() bool {
	_, ok := findSingboxPIDBySnapshot()
	return ok
}

// pidProcessIsOurSingbox returns true if pid is a live sing-box.exe.
// Uses OpenProcess instead of tasklist: tasklist is flaky on some Win10 builds
// (encoding, policy, timing) and caused false "process died" watchdog cleanups.
func pidProcessIsOurSingbox(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)

	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	if code != stillActive {
		return false
	}
	exe, ok := processImageBaseName(h)
	return ok && strings.EqualFold(exe, constants.ProcessExeName)
}

// processImageBaseName returns the image file name (e.g. "sing-box.exe") for an already-open
// process handle via QueryFullProcessImageName. This avoids a CreateToolhelp32Snapshot — which
// enumerates every process — on each call: the stale-PID retry path in isRunningRaw calls
// pidProcessIsOurSingbox several times, so a per-call snapshot meant up to ~6 full-process scans
// in one IsRunning() invocation.
func processImageBaseName(h windows.Handle) (string, bool) {
	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return "", false
	}
	full := windows.UTF16ToString(buf[:size])
	if full == "" {
		return "", false
	}
	if i := strings.LastIndexAny(full, `\/`); i >= 0 {
		full = full[i+1:]
	}
	return full, true
}

func findSingboxPIDBySnapshot() (int, bool) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, false
	}
	defer windows.CloseHandle(snap)

	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	target := strings.ToLower(constants.ProcessExeName)

	for err := windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		if strings.EqualFold(windows.UTF16ToString(pe.ExeFile[:]), target) {
			return int(pe.ProcessID), true
		}
	}
	return 0, false
}

// ResyncSingboxIfAlive restores .singbox_pid when sing-box is running but tracking was lost.
func ResyncSingboxIfAlive(dataDir string) bool {
	if pid, ok := findSingboxPIDBySnapshot(); ok && pidProcessIsOurSingbox(pid) {
		writeSingboxPIDFile(dataDir, pid)
		invalidateIsRunningCache()
		return true
	}
	return false
}
