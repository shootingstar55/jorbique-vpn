//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jorbiquevpn/internal/assets"
	"jorbiquevpn/internal/autoconnect"
	"jorbiquevpn/internal/config"
	"jorbiquevpn/internal/jsonorder"
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

// The persistent DNS cache also holds a selector choice once anything has
// switched the selector, as the local API of older builds did, and sing-box
// restores it over "default". The client's cache_id moved to a new namespace so
// such a leftover cannot bring the tunnel up on a protocol the scan did not pick.
func TestCachedSelectionFromAnOlderNamespaceIsIgnored(t *testing.T) {
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
	// The namespace the shipped template uses now.
	template, err := assets.FS.ReadFile("tun.json")
	if err != nil {
		t.Fatal(err)
	}
	var shipped struct {
		Experimental struct {
			CacheFile struct {
				CacheID string `json:"cache_id"`
			} `json:"cache_file"`
		} `json:"experimental"`
	}
	if err := json.Unmarshal(template, &shipped); err != nil {
		t.Fatal(err)
	}
	currentID := shipped.Experimental.CacheFile.CacheID
	if currentID == "" || currentID == "tun" {
		t.Fatalf("cache_id = %q, want a namespace other than the one older builds wrote choices into", currentID)
	}

	dir := t.TempDir()
	configPath := filepath.Join(dir, "automatic.json")
	writeConfig := func(cacheID string) {
		raw, _ := json.Marshal(map[string]any{
			"log": map[string]any{"disabled": true},
			"outbounds": []any{
				map[string]any{"type": "socks", "tag": "fast", "server": "127.0.0.1", "server_port": 1},
				map[string]any{"type": "socks", "tag": "slow", "server": "127.0.0.1", "server_port": 2},
				map[string]any{"type": "selector", "tag": "auto", "outbounds": []string{"fast", "slow"}, "default": "fast"},
			}, "route": map[string]any{"final": "auto"},
			"services": []any{map[string]any{"type": "api", "listen": "127.0.0.1", "listen_port": port, "secret": "test-secret"}},
			"experimental": map[string]any{"cache_file": map[string]any{
				"enabled": true, "path": filepath.Join(dir, "cache.db"), "cache_id": cacheID, "store_dns": true,
			}},
		})
		if err := os.WriteFile(configPath, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeConfig("tun")
	client, err := newAPIClient(configPath)
	if err != nil {
		t.Fatal(err)
	}
	start := func() func() {
		cmd := exec.Command(bin, "run", "-c", configPath)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		stop := func() {
			_ = cmd.Process.Signal(os.Interrupt)
			done := make(chan struct{})
			go func() { _ = cmd.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				_ = cmd.Process.Kill()
				<-done
			}
		}
		ready := time.Now().Add(5 * time.Second)
		for {
			if _, err := client.Groups(context.Background()); err == nil {
				break
			}
			if time.Now().After(ready) {
				stop()
				t.Fatal("API not ready")
			}
			time.Sleep(30 * time.Millisecond)
		}
		return stop
	}
	stop := start()
	if err := client.SelectOutbound(context.Background(), "auto", "slow"); err != nil {
		stop()
		t.Fatal(err)
	}
	stop()
	// Under the old namespace the choice comes back over "default"...
	stop = start()
	groups, err := client.Groups(context.Background())
	stop()
	if err != nil || len(groups) != 1 || groups[0].Selected != "slow" {
		t.Fatalf("cached choice was not restored: %+v %v", groups, err)
	}
	// ...and under the current one, from the same cache file, it does not.
	writeConfig(currentID)
	stop = start()
	defer stop()
	groups, err = client.Groups(context.Background())
	if err != nil || len(groups) != 1 || groups[0].Selected != "fast" {
		t.Fatalf("default not active: %+v %v", groups, err)
	}
}

// The verified group stays on the winner while it answers and moves to the
// next verified protocol once the winner stops answering.
func TestAutomaticGroupFailsOverOnlyWhenTheWinnerStops(t *testing.T) {
	bin := strings.TrimSpace(os.Getenv("SINGBOX_BIN"))
	if bin == "" {
		t.Skip("SINGBOX_BIN not set")
	}
	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	go func() {
		for {
			conn, err := target.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = conn.Read(make([]byte, 4096))
				// A real delay: the engine reads 0 ms as "no result yet".
				time.Sleep(20 * time.Millisecond)
				_, _ = conn.Write([]byte("HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n"))
			}()
		}
	}()
	freePort := func() int {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		return l.Addr().(*net.TCPAddr).Port
	}
	run := func(winner map[string]any) string {
		t.Helper()
		dir := t.TempDir()
		configPath := filepath.Join(dir, "failover.json")
		port := freePort()
		raw, _ := json.Marshal(map[string]any{
			"log": map[string]any{"disabled": true},
			"outbounds": []any{
				winner,
				map[string]any{"type": "direct", "tag": "backup"},
				map[string]any{"type": "urltest", "tag": "auto", "outbounds": []string{"backup", "winner"}, "url": "http://" + target.Addr().String() + "/"},
				map[string]any{"type": "selector", "tag": "proxy", "outbounds": []string{"auto"}},
			}, "route": map[string]any{"final": "proxy"},
			"services": []any{map[string]any{"type": "api", "listen": "127.0.0.1", "listen_port": port, "secret": "test-secret"}},
		})
		root := jsonorder.New()
		if err := jsonorder.Unmarshal(raw, root); err != nil {
			t.Fatal(err)
		}
		selection := autoconnect.Result{Working: []autoconnect.Measurement{{Tag: "winner"}, {Tag: "backup"}}}
		selection.ConfigureGroups(root)
		body, err := jsonorder.MarshalIndent(root)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(configPath, body, 0600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(bin, "run", "-c", configPath)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
		client, err := newAPIClient(configPath)
		if err != nil {
			t.Fatal(err)
		}
		// The first check runs right after startup; give it time to finish.
		deadline := time.Now().Add(8 * time.Second)
		var last []apiGroup
		for time.Now().Before(deadline) {
			groups, err := client.Groups(context.Background())
			if err == nil {
				last = groups
				tested := false
				selected := map[string]string{}
				for _, g := range groups {
					selected[g.Tag] = g.Selected
					if g.Tag == "auto" {
						for _, item := range g.Items {
							tested = tested || item.URLTestTime > 0
						}
					}
				}
				if tested && selected["proxy"] == "auto" {
					return selected["auto"]
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("no urltest result: %+v\n%s", last, stderr.String())
		return ""
	}
	if got := run(map[string]any{"type": "direct", "tag": "winner"}); got != "winner" {
		t.Fatalf("live winner replaced by %q", got)
	}
	dead := map[string]any{"type": "socks", "tag": "winner", "server": "127.0.0.1", "server_port": freePort()}
	if got := run(dead); got != "backup" {
		t.Fatalf("dead winner kept: %q", got)
	}
}
