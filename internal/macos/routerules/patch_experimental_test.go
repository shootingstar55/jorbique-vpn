package routerules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"jorbiquevpn/internal/macos/singbox"
)

// The sing-box start path injects experimental.cache_file (the persistent DNS
// cache) into the generated config, and the route-rules re-apply rewrites the
// file right after — the section must survive that rewrite.
func TestPatchSingboxRouteRulesPreservesExperimental(t *testing.T) {
	root := singbox.NewOrderedMap()
	if err := root.UnmarshalJSON([]byte(`{
		"dns": {"servers": [], "rules": []},
		"inbounds": [],
		"outbounds": [{"type": "direct", "tag": "direct"}],
		"route": {"rules": []}
	}`)); err != nil {
		t.Fatal(err)
	}
	cacheFile := singbox.NewOrderedMap()
	cacheFile.Set("enabled", true)
	cacheFile.Set("path", "/opt/old/cache/jorbique_vpn/cache.db")
	cacheFile.Set("cache_id", "jorbique_vpn")
	cacheFile.Set("store_fakeip", false)
	cacheFile.Set("store_dns", true)
	experimental := singbox.NewOrderedMap()
	experimental.Set("cache_file", cacheFile)
	root.Set("experimental", experimental)

	path := filepath.Join(t.TempDir(), "singbox.json")
	if err := singbox.WriteJSONFileAtomically(root, path); err != nil {
		t.Fatal(err)
	}

	if err := PatchSingboxRouteRules(path, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	final, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(final, &parsed); err != nil {
		t.Fatalf("patched config is not valid JSON: %v", err)
	}
	exp, ok := parsed["experimental"].(map[string]any)
	if !ok {
		t.Fatal("experimental section lost after route-rules patch")
	}
	cf, ok := exp["cache_file"].(map[string]any)
	if !ok {
		t.Fatal("cache_file lost after route-rules patch")
	}
	if cf["path"] != "/opt/old/cache/jorbique_vpn/cache.db" || cf["enabled"] != true || cf["cache_id"] != "jorbique_vpn" {
		t.Fatalf("cache_file mangled: %v", cf)
	}
}
