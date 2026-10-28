//go:build windows

package main

import (
	"jorbiquevpn/internal/clientupdate"
	"strconv"
)

func applyClientUpdate(args []string) (bool, error) {
	if len(args) == 0 || args[0] != "--apply-client-update" {
		return false, nil
	}
	if len(args) != 2 {
		return true, strconv.ErrSyntax
	}
	pid, err := strconv.ParseUint(args[1], 10, 32)
	if err != nil || pid == 0 {
		return true, strconv.ErrSyntax
	}
	return true, clientupdate.ApplyWindows(uint32(pid))
}
