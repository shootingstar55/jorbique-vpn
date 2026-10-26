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

// IsRunning reports whether an instance holds the mutex, without taking it. A
// jorbique:// link starts a second, unelevated copy of the client; it uses this
// to hand the link over to the running one instead of starting the app again.
// The running copy is elevated, so its mutex may refuse an unelevated opener:
// a refusal still means it exists.
func IsRunning() bool {
	name, err := windows.UTF16PtrFromString(mutexName)
	if err != nil {
		return false
	}
	handle, err := windows.OpenMutex(windows.SYNCHRONIZE, false, name)
	if err == nil {
		_ = windows.CloseHandle(handle)
		return true
	}
	return err == windows.ERROR_ACCESS_DENIED
}
