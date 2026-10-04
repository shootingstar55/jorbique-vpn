package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jorbiquevpn/internal/jsonorder"
)

func TestRemoteSingboxCachePath_UnderTempAppDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp) // os.TempDir() honors TMPDIR on darwin/linux
	p := RemoteSingboxCachePath()
	if filepath.Base(p) != "singbox_client.json" {
		t.Errorf("cache file = %q", p)
	}
	// The parent directory is created as a side effect.
	if st, err := os.Stat(filepath.Dir(p)); err != nil || !st.IsDir() {
		t.Errorf("cache dir not created: %v", err)
	}
}

func TestLoadRemoteCacheOrErr(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	p := RemoteSingboxCachePath()

	// No cache present -> error mentions the cause and the path.
	if _, err := loadRemoteCacheOrErr(os.ErrNotExist, nil); err == nil || !strings.Contains(err.Error(), "no valid cache") {
		t.Fatalf("expected no-cache error, got %v", err)
	}

	// Invalid JSON cache -> still treated as missing.
	if err := os.WriteFile(p, []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRemoteCacheOrErr(nil, nil); err == nil {
		t.Fatal("invalid JSON cache should be rejected")
	}

	// Valid JSON cache -> returned verbatim.
	want := []byte(`{"outbounds":[]}`)
	if err := os.WriteFile(p, want, 0644); err != nil {
		t.Fatal(err)
	}
	got, err := loadRemoteCacheOrErr(nil, nil)
	if err != nil || string(got) != string(want) {
		t.Fatalf("valid cache: got %q err %v", got, err)
	}
}

func TestFetchSingboxClientJSONOrCache_FallsBackToCache(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	// Force the network call to fail fast instead of waiting on the real host.
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")

	want := []byte(`{"outbounds":[{"tag":"x"}]}`)
	if err := os.WriteFile(RemoteSingboxCachePath(), want, 0644); err != nil {
		t.Fatal(err)
	}
	// A connection built from a stale profile is indistinguishable from one
	// built from a fresh profile unless the fallback says so.
	var warned string
	got, err := FetchSingboxClientJSONOrCache(func(m string) { warned = m })
	if err != nil {
		t.Fatalf("expected cache fallback to succeed, got err %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("fallback returned %q, want %q", got, want)
	}
	if warned == "" {
		t.Error("the fallback to a stale profile was silent")
	}
}

func TestFetchSingboxClientJSONOrCache_NoCacheReturnsError(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	if _, err := FetchSingboxClientJSONOrCache(nil); err == nil {
		t.Fatal("expected error when both network and cache are unavailable")
	}
}

func TestFixPathsInConfig_RewritesLogAndCachePaths(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	cfg := jsonorder.FromMap(map[string]any{
		"log": jsonorder.FromMap(map[string]any{"output": "logs/tun_system.log"}),
		"experimental": jsonorder.FromMap(map[string]any{
			"cache_file": jsonorder.FromMap(map[string]any{"path": "cache/tun_system/cache.db"}),
		}),
	})
	fixPathsInConfig(cfg)

	out := field[string](field[*jsonorder.Map](cfg, "log"), "output")
	if !strings.HasSuffix(out, "logs/tun_system.log") || !strings.Contains(out, "JorbiqueVPN") {
		t.Errorf("log output not rewritten to abs data dir: %q", out)
	}
	cf := field[string](field[*jsonorder.Map](field[*jsonorder.Map](cfg, "experimental"), "cache_file"), "path")
	if !strings.HasSuffix(cf, "cache/tun_system/cache.db") || !strings.Contains(cf, "JorbiqueVPN") {
		t.Errorf("cache path not rewritten: %q", cf)
	}

	// A path that came from another machine keeps only its file name: the whole
	// profile is taken as the server sends it, so an absolute path in it is
	// somebody else's disk, not an instruction about ours.
	cfg2 := jsonorder.FromMap(map[string]any{"log": jsonorder.FromMap(map[string]any{"output": "C:/Users/Someone/AppData/Roaming/JorbiqueVPN/logs/x.log"})})
	fixPathsInConfig(cfg2)
	out2 := field[string](field[*jsonorder.Map](cfg2, "log"), "output")
	if !strings.HasSuffix(out2, "logs/x.log") || strings.Contains(out2, "Someone") {
		t.Errorf("a foreign log path was kept: %q", out2)
	}
	fixPathsInConfig(jsonorder.FromMap(map[string]any{})) // no log/experimental
}

func TestOutboundEnabled_Defaults(t *testing.T) {
	if !OutboundEnabled(nil, "tuic") {
		t.Error("nil map should default to enabled")
	}
	if !OutboundEnabled(map[string]bool{}, "tuic") {
		t.Error("missing key should default to enabled")
	}
	if OutboundEnabled(map[string]bool{"tuic": false}, "tuic") {
		t.Error("explicit false should disable")
	}
	if !OutboundEnabled(map[string]bool{"tuic": true}, "tuic") {
		t.Error("explicit true should enable")
	}
}

func TestNormalizeHysteria2ServerPorts_Variants(t *testing.T) {
	cfg := jsonorder.FromMap(map[string]any{
		"outbounds": []any{
			jsonorder.FromMap(map[string]any{"type": "hysteria2", "server_ports": []any{"2080-3000", "443:443", 123}}),
			jsonorder.FromMap(map[string]any{"type": "vless"}),     // ignored: not hysteria2
			jsonorder.FromMap(map[string]any{"type": "hysteria2"}), // no server_ports: skipped
			"not-a-map",
		},
	})
	NormalizeHysteria2ServerPorts(cfg)
	sp := field[[]any](field[[]any](cfg, "outbounds")[0].(*jsonorder.Map), "server_ports")
	if sp[0] != "2080:3000" {
		t.Errorf("hyphen range not converted: %v", sp[0])
	}
	if sp[1] != "443:443" {
		t.Errorf("colon range altered: %v", sp[1])
	}

	NormalizeHysteria2ServerPorts(nil)                                                 // no panic
	NormalizeHysteria2ServerPorts(jsonorder.FromMap(map[string]any{}))                 // no outbounds
	NormalizeHysteria2ServerPorts(jsonorder.FromMap(map[string]any{"outbounds": "x"})) // wrong type
}
