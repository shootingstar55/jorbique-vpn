package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssignFreshTunNameRewritesAndPreserves(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "runtime_config.json")
	original := `{
		"log": {"level": "warn", "output": "logs/tun.log"},
		"inbounds": [{"type": "tun", "tag": "tun-in", "interface_name": "singtun", "address": ["100.64.0.2/24"], "mtu": 1280, "auto_route": false}],
		"outbounds": [{"type": "vless", "server_port": 443}]
	}`
	if err := os.WriteFile(cfgPath, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	assignFreshTunName(cfgPath)

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("rewritten config is not valid JSON: %v\n%s", err, data)
	}
	inbounds, ok := cfg["inbounds"].([]interface{})
	if !ok || len(inbounds) == 0 {
		t.Fatalf("inbounds missing after rewrite: %s", data)
	}
	tun := inbounds[0].(map[string]interface{})
	name, _ := tun["interface_name"].(string)
	if name == "" || name == "singtun" {
		t.Fatalf("interface_name not made unique, got %q", name)
	}
	if !strings.HasPrefix(name, "singtun") {
		t.Fatalf("interface_name lost its prefix: %q", name)
	}
	// Other fields preserved.
	if tun["stack"] != nil && tun["tag"] != "tun-in" {
		t.Fatalf("tag not preserved: %v", tun["tag"])
	}
	if tun["mtu"] == nil {
		t.Fatalf("mtu dropped after rewrite")
	}
	// mtu must stay numeric 1280 (UseNumber keeps it integer, not 1.28e3 etc.).
	if got := strings.TrimSpace(string(data)); !strings.Contains(got, "1280") {
		t.Fatalf("mtu reformatted unexpectedly: %s", got)
	}
	if cfg["log"] == nil || cfg["outbounds"] == nil {
		t.Fatalf("top-level sections dropped: %s", data)
	}
	// The rewrite must preserve sing-box key order and pretty-printing, not fall back to
	// json.Marshal's alphabetical, minified output. Alphabetical order would put "inbounds"
	// before "log"; the ordered writer keeps log -> inbounds -> outbounds.
	got := string(data)
	if !strings.Contains(got, "\n") {
		t.Fatalf("config was minified, expected pretty-printed ordered output: %s", got)
	}
	logAt := strings.Index(got, `"log"`)
	inboundsAt := strings.Index(got, `"inbounds"`)
	outboundsAt := strings.Index(got, `"outbounds"`)
	if !(logAt >= 0 && logAt < inboundsAt && inboundsAt < outboundsAt) {
		t.Fatalf("top-level keys not in sing-box order (log<inbounds<outbounds): %s", got)
	}
}

func TestAssignFreshTunNameUniquePerCall(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "c.json")
	write := func() {
		_ = os.WriteFile(cfgPath, []byte(`{"inbounds":[{"type":"tun","interface_name":"singtun"}]}`), 0644)
	}
	read := func() string {
		data, _ := os.ReadFile(cfgPath)
		var cfg map[string]interface{}
		_ = json.Unmarshal(data, &cfg)
		ib := cfg["inbounds"].([]interface{})[0].(map[string]interface{})
		return ib["interface_name"].(string)
	}
	write()
	assignFreshTunName(cfgPath)
	first := read()
	// Re-seed and reassign; the rewrite must be able to change the name again.
	assignFreshTunName(cfgPath)
	second := read()
	if first == "" || second == "" {
		t.Fatalf("empty names: %q %q", first, second)
	}
	// Two back-to-back assignments must differ — the whole point of the unique-name feature is that
	// a retry never collides with the adapter the previous attempt may have leaked.
	if first == second {
		t.Fatalf("interface_name not unique across calls: both %q", first)
	}
}

func TestNextTunNameUniqueAcrossRapidCalls(t *testing.T) {
	const n = 1000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		name := nextTunName()
		if !strings.HasPrefix(name, "singtun") {
			t.Fatalf("name lost prefix: %q", name)
		}
		if _, dup := seen[name]; dup {
			t.Fatalf("duplicate TUN name within a process: %q", name)
		}
		seen[name] = struct{}{}
	}
}
