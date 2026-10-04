package singbox

import (
	"jorbiquevpn/internal/macos/core"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Deleting the config used to leave the client unable to start: the local half
// was read from that file, so with no file there was no TUN inbound and no log.
// It is built here instead, the way the Windows client builds its own from an
// embedded template — the downloaded profile is enough on its own.
func TestConfigIsBuiltWithNoLocalFileToReadFrom(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "singbox.json") // deliberately absent
	if err := os.WriteFile(filepath.Join(dir, "singbox_client.json"), []byte(selectorProfileJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	root, err := BuildConfigFromRemoteTemplate(core.SingboxSection{UUID: "11111111-1111-1111-1111-111111111111"}, base, time.Nanosecond, nil)
	if err != nil {
		t.Fatalf("BuildConfigFromRemoteTemplate: %v", err)
	}

	// Everything sing-box needs that the profile does not carry.
	for _, section := range []string{"log", "inbounds", "services"} {
		if _, ok := root.Get(section); !ok {
			t.Errorf("%q is missing from a config built without a local file", section)
		}
	}
	inbounds, _ := root.Get("inbounds")
	list, _ := inbounds.([]any)
	if len(list) != 1 {
		t.Fatalf("inbounds = %v", inbounds)
	}
	tun := list[0].(*OrderedMap)
	if tun.GetString("type") != "tun" || tun.GetString("tag") != "tun-in" {
		t.Errorf("the inbound is not the TUN one: %v", tun.Keys())
	}
	// And what the profile does carry is still there.
	if _, ok := root.Get("outbounds"); !ok {
		t.Error("the profile's outbounds were lost")
	}
}

// The panels read the profile out of a cache beside the config. A machine whose
// config directory was deleted has neither, and the fetch has to create the
// directory rather than fail on it — otherwise the VPN Config window stays empty
// until someone connects, which is what it used to do.
func TestRefreshCreatesTheDirectoryItCachesInto(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "cfg", "singbox.json")

	// No network in a unit test: the fetch fails, and the point is where it got
	// to before failing.
	_ = RefreshRemoteTemplate(base, time.Nanosecond, nil)

	if _, err := os.Stat(filepath.Dir(base)); err != nil {
		t.Errorf("the cache directory was not created: %v", err)
	}
}
