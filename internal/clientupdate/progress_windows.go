//go:build windows

package clientupdate

import (
	"runtime"
	"syscall"
	"unsafe"

	ole "github.com/go-ole/go-ole"
	"golang.org/x/sys/windows"
)

// The shell owns a background UI thread, so feedback survives the old app's exit.
// https://learn.microsoft.com/windows/win32/api/shlobj_core/nn-shlobj_core-iprogressdialog
func showRestartProgress() (line func(string), closeDialog func()) {
	line, closeDialog = func(string) {}, func() {}
	runtime.LockOSThread()
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		runtime.UnlockOSThread()
		return
	}
	dialog, err := ole.CreateInstance(ole.NewGUID("{F8383852-FCD3-11D1-A6B9-006097DF5BD4}"), ole.NewGUID("{EBBC7C04-315E-11D2-B62F-006097DF5BD4}"))
	if err != nil {
		ole.CoUninitialize()
		runtime.UnlockOSThread()
		return
	}
	vt := *(**[13]uintptr)(unsafe.Pointer(dialog))
	self := uintptr(unsafe.Pointer(dialog))
	title, _ := windows.UTF16PtrFromString("Обновление Jorbique VPN")
	syscall.SyscallN(vt[5], self, uintptr(unsafe.Pointer(title))) // SetTitle
	// NOTIME | NOMINIMIZE | MARQUEEPROGRESS | NOCANCEL: replacement is atomic.
	hr, _, _ := syscall.SyscallN(vt[3], self, 0, 0, 0x04|0x08|0x20|0x40, 0)
	if int32(hr) < 0 {
		dialog.Release()
		ole.CoUninitialize()
		runtime.UnlockOSThread()
		return
	}
	line = func(text string) {
		value, _ := windows.UTF16PtrFromString(text)
		syscall.SyscallN(vt[10], self, 1, uintptr(unsafe.Pointer(value)), 0, 0)
	}
	closeDialog = func() {
		syscall.SyscallN(vt[4], self)
		dialog.Release()
		ole.CoUninitialize()
		runtime.UnlockOSThread()
	}
	line("Устанавливаем обновление. Клиент откроется автоматически…")
	return
}
