//go:build windows

package singleinstance

import (
	"golang.org/x/sys/windows"
)

const mutexName = "Global\\JorbiqueVPN_AppInstance"

// TryMutex creates a named mutex; if another instance holds it, returns ok=false.
// Call release on success before exit.
func TryMutex() (release func(), ok bool) {
	name, err := windows.UTF16PtrFromString(mutexName)
	if err != nil {
		return nil, false
	}
	handle, err := windows.CreateMutex(nil, false, name)
	if err != nil {
		if err == windows.ERROR_ALREADY_EXISTS {
			if handle != 0 {
				_ = windows.CloseHandle(handle)
			}
			return nil, false
		}
		return nil, false
	}
	return func() {
		_ = windows.CloseHandle(handle)
	}, true
}
