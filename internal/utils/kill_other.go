//go:build !windows

package utils

func TerminateByPID(_ int) error { return nil }

func TerminateByName(_ string) int { return 0 }
