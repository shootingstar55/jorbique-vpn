//go:build !windows

package network

func writeNrptRulesNative(_ []nrptRule) error { return nil }

func removeNrptRulesNative() error { return nil }
