//go:build windows

package utils

import (
	"os/exec"
	"syscall"
)

// CREATE_NO_WINDOW = 0x08000000 - prevents console window from appearing
const createNoWindow = 0x08000000

// HideCommandWindow prevents cmd/powershell windows from appearing
func HideCommandWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags = createNoWindow
}
