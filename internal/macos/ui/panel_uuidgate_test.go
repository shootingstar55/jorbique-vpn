//go:build darwin

package ui

import "testing"

func TestUUIDGateErrorShowsTheChildsReason(t *testing.T) {
	for stderr, want := range map[string]string{
		"\x1b[31mBad config: This user UUID is not registered on the server\n\x1b[0m\x1b[33mHint: Check vpn_cfg.json\n\x1b[0m": "This user UUID is not registered on the server",
		"\n\x1b[31mPermission error: set-singbox-config needs the Jorbique VPN Extended daemon, which is not running.\x1b[0m\n":         "Permission error: set-singbox-config needs the Jorbique VPN Extended daemon, which is not running.",
		// What a fresh install writes before the refusal: no sing-box yet, and
		// the check's own diagnostic when the server cannot be reached.
		"\x1b[33m[2026-09-22T10:00:00+03:00] WARN: tool not found: sing-box (install via brew)\x1b[0m\n\x1b[31mBad config: This user UUID is not registered on the server\n\x1b[0m\x1b[33mHint: Check vpn_cfg.json\n\x1b[0m":     "This user UUID is not registered on the server",
		"[2026-09-22T10:00:00+03:00] WARN: User UUID check: failed to upload metrics: Post \"https://example.invalid/otlp/v1/metrics\": dial tcp: no such host\nBad config: Could not reach the server to check the user UUID\n": "Could not reach the server to check the user UUID",
		"": "Could not save the user UUID",
	} {
		if got := uuidGateError(stderr); got != want {
			t.Errorf("uuidGateError(%q) = %q, want %q", stderr, got, want)
		}
	}
}
