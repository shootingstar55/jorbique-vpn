//go:build !windows

package main

func applyClientUpdate(args []string) (bool, error) { return false, nil }
