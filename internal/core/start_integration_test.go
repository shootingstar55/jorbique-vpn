//go:build !windows

package core

import (
	"os"
	"path/filepath"
	"testing"

	"jorbiquevpn/internal/config"
)

// writeFakeSingbox creates an executable stub that mimics the sing-box CLI surface
// (check/run/version). It lets Start run end-to-end without the real binary. The
// process is never recognized as "our sing-box" by the non-Windows process stub,
// so Start exercises the launch + retry + failure path (it never reaches route setup,
// which would require touching the OS routing table).
func writeFakeSingbox(t *testing.T, checkExit int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sing-box")
	script := "#!/bin/sh\n" +
		"case \"$1\" in\n" +
		"  check) exit " + itoa(checkExit) + " ;;\n" +
		"  run) exit 0 ;;\n" +
		"  version) echo 'sing-box version 1.13.7 (test)' ;;\n" +
		"esac\n" +
		"exit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	return "1"
}

func fastConfig() config.Config {
	return config.Config{GatewayIP: "100.64.0.1", MaxStartupChecks: 1, StartupCheckDelay: 1}
}

func writeRuntimeConfig(t *testing.T, dataDir string) string {
	t.Helper()
	cfgPath := filepath.Join(dataDir, "runtime_config.json")
	body := `{"log":{"level":"warn","output":"logs/tun.log"},"inbounds":[{"type":"tun","interface_name":"singtun","address":["100.64.0.2/24"]}],"outbounds":[{"type":"direct"}]}`
	if err := os.WriteFile(cfgPath, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

func TestStart_ConfigNotFound(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	ok, msg := Start("does-not-exist.json", t.TempDir(), "sing-box",
		fastConfig(), func(string, string) {})
	if ok {
		t.Error("missing config should fail")
	}
	if msg == "" {
		t.Error("expected an error message")
	}
}

func TestStart_ConfigCheckFails(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("APPDATA", dataDir)
	exe := writeFakeSingbox(t, 1) // `check` exits non-zero
	cfgPath := writeRuntimeConfig(t, dataDir)

	ok, msg := Start(cfgPath, dataDir, exe,
		fastConfig(), func(string, string) {})
	if ok {
		t.Error("failed config check should fail Start")
	}
	if msg == "" {
		t.Error("expected config-check error message")
	}
}

func TestStart_LaunchPathButNotConfirmed(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("APPDATA", dataDir)
	exe := writeFakeSingbox(t, 0)
	cfgPath := writeRuntimeConfig(t, dataDir)
	invalidateIsRunningCache()

	// The launched stub is never recognized as our sing-box, so Start retries and
	// ultimately reports failure — without ever touching the routing table.
	ok, _ := Start(cfgPath, dataDir, exe,
		fastConfig(), func(string, string) {})
	if ok {
		t.Error("Start should report failure when the process is never confirmed running")
	}
}
