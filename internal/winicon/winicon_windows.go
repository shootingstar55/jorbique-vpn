//go:build windows

package winicon

import (
	"os"
	"path/filepath"
	"time"
	"unsafe"

	"jorbiquevpn/internal/constants"

	"golang.org/x/sys/windows"
)

const (
	wmSetIcon = 0x0080
	iconSmall = 0
	iconBig   = 1
)

var (
	user32           = windows.NewLazySystemDLL("user32.dll")
	shell32          = windows.NewLazySystemDLL("shell32.dll")
	procFindWindowW  = user32.NewProc("FindWindowW")
	procSendMessageW = user32.NewProc("SendMessageW")
	procExtractIconW = shell32.NewProc("ExtractIconW")
)

// ApplyTitleBarFromExeDir sets WM_SETICON on the main window using an .ico next to the exe.
// Fallback when embedded RT_GROUP_ICON #3 is missing (Wails uses AppIconID=3).
func ApplyTitleBarFromExeDir(exeDir string) {
	go func() {
		for _, delay := range []time.Duration{200 * time.Millisecond, 500 * time.Millisecond, 1200 * time.Millisecond} {
			time.Sleep(delay)
			if applyOnce(exeDir) {
				return
			}
		}
	}()
}

func applyOnce(exeDir string) bool {
	hwnd := findMainWindow()
	if hwnd == 0 {
		return false
	}
	for _, name := range []string{constants.AppIconICO} {
		path := filepath.Join(exeDir, name)
		if hicon := extractIcon(path); hicon != 0 {
			setWindowIcons(hwnd, hicon)
			return true
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if hicon := extractIcon(exe); hicon != 0 {
		setWindowIcons(hwnd, hicon)
		return true
	}
	return false
}

func findMainWindow() uintptr {
	title, _ := windows.UTF16PtrFromString(constants.AppDisplayName)
	r, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(title)))
	return r
}

func extractIcon(path string) uintptr {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0
	}
	r, _, _ := procExtractIconW.Call(0, uintptr(unsafe.Pointer(p)), 0)
	if r == 0 || r == 1 {
		return 0
	}
	return r
}

func setWindowIcons(hwnd, hicon uintptr) {
	procSendMessageW.Call(hwnd, wmSetIcon, iconSmall, hicon)
	procSendMessageW.Call(hwnd, wmSetIcon, iconBig, hicon)
}
