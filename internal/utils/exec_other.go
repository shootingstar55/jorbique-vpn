//go:build !windows

package utils

import "os/exec"

func HideCommandWindow(cmd *exec.Cmd) {
	// No-op on non-Windows
	_ = cmd
}
