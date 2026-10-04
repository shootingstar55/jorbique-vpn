//go:build !windows

package core

func singboxProcessExistsByImageName() bool { return false }

func pidProcessIsOurSingbox(pid int) bool { return false }

func findSingboxPIDBySnapshot() (int, bool) { return 0, false }

func ResyncSingboxIfAlive(dataDir string) bool { return false }
