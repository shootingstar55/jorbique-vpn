//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"golang.org/x/sys/windows"

	"jorbiquevpn/internal/constants"
)

// Elevation and the two startup dialogs used to be PowerShell one-liners: Start-Process
// -Verb RunAs, and [System.Windows.MessageBox]::Show. Both are wrappers over shell32 and
// user32, and the wrapper was what made quoting — doubling every apostrophe in a path, an
// argument or an error message — part of the startup path. Calling the two functions
// directly also drops a PowerShell launch from the first second of every cold start, which
// is the second the user is watching.

// swShowNormal is what a relaunched GUI client wants: the -WindowStyle the old command
// carried was PowerShell's own window, not ours.
const swShowNormal = 1

// isAdmin reports whether this process already runs elevated. `net session` answered the
// same question by failing, which is what the token says without starting anything.
func isAdmin() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

func showErrorBox(msg string) {
	messageBox(constants.AppDisplayName+" - Error", msg)
}

func showAdminMessageBox() {
	messageBox(constants.AppDisplayName, "PLEASE RUN AS ADMINISTRATOR")
}

func messageBox(title, text string) {
	caption, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	body, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	// No parent window: this runs before, or instead of, the Wails window.
	_, _ = windows.MessageBox(0, body, caption, windows.MB_OK|windows.MB_ICONERROR|windows.MB_SETFOREGROUND)
}

// relaunchElevated starts this executable again through the UAC prompt and returns once the
// user has answered it. A refused prompt is an error, as the failed PowerShell run was.
func relaunchElevated() error {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return errors.New("executable path not found")
	}
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}
	// ComposeCommandLine applies the quoting rules CommandLineToArgvW reads back, so an
	// argument with a space or a quote in it survives the trip.
	args, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(os.Args[1:]))
	if err != nil {
		return err
	}
	cwd, err := windows.UTF16PtrFromString(filepath.Dir(exe))
	if err != nil {
		return err
	}
	// ShellExecute wants an initialized apartment on the calling thread, and it is the
	// thread's apartment that the shell talks to.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); err == nil {
		defer windows.CoUninitialize()
	}
	if err := windows.ShellExecute(0, verb, file, args, cwd, swShowNormal); err != nil {
		return fmt.Errorf("runas: %w", err)
	}
	return nil
}
