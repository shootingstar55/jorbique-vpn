//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jorbiquevpn/internal/config"
	"jorbiquevpn/internal/dashboard"
)

// sampleRemoteProfile mirrors the shape of the profile the update server returns:
// dns/outbounds/route that get merged onto the embedded tun template.
const sampleRemoteProfile = `{
  "dns": {"servers": [{"tag": "dns-remote", "type": "udp", "server": "1.1.1.1"}]},
  "outbounds": [
    {"tag": "vless-reality", "type": "vless", "server": "example.com", "server_port": 443, "uuid": "", "tls": {"enabled": true, "server_name": "example.com"}},
    {"tag": "tuic", "type": "tuic", "server": "example.com", "server_port": 443, "uuid": "", "password": "", "tls": {"enabled": true, "server_name": "example.com"}},
    {"tag": "hysteria2", "type": "hysteria2", "server": "example.com", "server_port": 443, "password": "", "tls": {"enabled": true, "server_name": "example.com"}},
    {"tag": "vless", "type": "urltest", "outbounds": ["vless-reality"]},
    {"tag": "auto", "type": "urltest", "outbounds": ["vless-reality", "tuic", "hysteria2"]},
    {"tag": "direct", "type": "direct"}
  ],
  "route": {"rules": [
    {"protocol": "dns", "action": "hijack-dns"},
    {"outbound": "direct", "action": "route", "source_ip_cidr": ["10.8.0.0/16"]},
    {"outbound": "direct", "action": "route", "ip_is_private": true},
    {"outbound": "direct", "action": "route", "domain_regex": [".*\\.ru$"]}
  ]}
}`

// TestEndToEndConfigBuild builds a runtime config through the real pipeline and,
// when SINGBOX_BIN is provided, validates it with the actual sing-box binary.
func TestEndToEndConfigBuild(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	const uuid = "11111111-1111-1111-1111-111111111111"
	enabled := map[string]bool{
		"vless-reality": true, "vless-grpc": true, "tuic": true, "hysteria2": true, "anytls": true,
	}

	outPath, err := config.BuildRuntimeConfig(
		[]byte(sampleRemoteProfile), config.RuntimeOptions{
			UserUUID:            uuid,
			OutboundEnabled:     enabled,
			IgnoredProcessNames: []string{"chrome"},
			IgnoredProcessPaths: []string{filepath.FromSlash("/usr/bin/zoom")},
			SelectedNode:        "tuic",
			UserDirectDomains:   []string{"example.com"},
		},
	)
	if err != nil {
		t.Fatalf("BuildRuntimeConfig: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("generated config is not valid JSON: %v", err)
	}
	if strings.Contains(string(data), `"uuid": ""`) || strings.Contains(string(data), `"password": ""`) {
		t.Error("the empty credential was not filled in")
	}

	validateWithSingbox(t, outPath)
}

func TestDashboardReadsRealStartedService(t *testing.T) {
	bin := strings.TrimSpace(os.Getenv("SINGBOX_BIN"))
	if bin == "" {
		t.Skip("SINGBOX_BIN not set")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	configPath := filepath.Join(t.TempDir(), "dashboard.json")
	configData := []byte(fmt.Sprintf(`{"log":{"disabled":true},"services":[{"type":"api","tag":"local-api","listen":"127.0.0.1","listen_port":%d,"secret":"integration-secret"}]}`, port))
	if err := os.WriteFile(configPath, configData, 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	cmd := exec.Command(bin, "run", "-c", configPath) //nolint:gosec // explicit test binary
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	client, err := dashboard.NewClient(configPath)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		snapshot, err := client.Snapshot(context.Background())
		if err == nil {
			if snapshot.Version != "1.14.0" {
				t.Fatalf("API version = %q", snapshot.Version)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("StartedService did not become ready: %v\n%s", err, output.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// validateWithSingbox runs `sing-box check` against the generated config when a
// binary path is provided via SINGBOX_BIN; otherwise it is skipped.
func validateWithSingbox(t *testing.T, configPath string) {
	t.Helper()
	bin := strings.TrimSpace(os.Getenv("SINGBOX_BIN"))
	if bin == "" {
		t.Log("SINGBOX_BIN not set; skipping real sing-box validation")
		return
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("SINGBOX_BIN %q not found: %v", bin, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Test-only: bin comes from the SINGBOX_BIN env var set by CI/developers.
	cmd := exec.CommandContext(ctx, bin, "check", "-c", configPath) // nosemgrep
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sing-box check failed: %v\n%s", err, out)
	}
}
