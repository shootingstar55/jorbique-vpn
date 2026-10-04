//go:build windows

package utils

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Process termination through the Win32 API, where taskkill /F used to run. taskkill is a
// process launch — and one that is bounded on a synchronous path (Stop, and the connect
// preflight) because an AV-held target can make it hang. OpenProcess + TerminateProcess is the
// call taskkill itself makes, with no child process to wait on.

// TerminateByPID force-kills one process. A process that is already gone is not an error.
func TerminateByPID(pid int) error {
	if pid <= 0 {
		return nil
	}
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return nil // exited, or never existed
	}
	defer windows.CloseHandle(h)
	return windows.TerminateProcess(h, 1)
}

// TerminateByName force-kills every running process with this image name and reports how many
// it terminated.
func TerminateByName(exeName string) int {
	exeName = strings.TrimSpace(exeName)
	if exeName == "" {
		return 0
	}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(snap)

	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	killed := 0
	for err := windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		if !strings.EqualFold(windows.UTF16ToString(pe.ExeFile[:]), exeName) {
			continue
		}
		if TerminateByPID(int(pe.ProcessID)) == nil {
			killed++
		}
	}
	return killed
}
