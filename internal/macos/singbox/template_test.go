package singbox

import (
	"encoding/json"
	"jorbiquevpn/internal/macos/core"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRemoteTemplateKeepsOutboundTopology(t *testing.T) {
	const profile = `{"outbounds":[
		{"type":"vless","tag":"a","uuid":"","detour":"transport"},
		{"type":"vless","tag":"b","uuid":""},
		{"type":"urltest","tag":"regional","outbounds":["a","b"]},
		{"type":"tuic","tag":"c","password":""},
		{"type":"anytls","tag":"new","password":""},
		{"type":"urltest","tag":"auto","outbounds":["c","regional"]},
		{"type":"selector","tag":"pick","outbounds":["regional","c"],"default":"regional"},
		{"type":"shadowtls","tag":"transport","password":"server-owned"},
		{"type":"direct","tag":"direct"},
		{"type":"block","tag":"reject"},
		{"type":"dns","tag":"dns-out"}
	],"route":{"final":"auto","rules":[{"outbound":"reject"},{"outbound":"dns-out"},{"outbound":"pick"}]}}`
	available := ProxyTagsFromOutbounds(parseOutbounds(t, profile), nil)
	if want := []string{"c", "a", "b", "new"}; !reflect.DeepEqual(available, want) {
		t.Errorf("available = %v, want %v", available, want)
	}
	for _, tc := range []struct {
		name     string
		disabled []string
		auto     []any
	}{
		{"all", nil, []any{"c", "regional", "new"}},
		{"empty regional group", []string{"a", "b"}, []any{"c", "new"}},
		{"all disabled", []string{"a", "b", "c", "new"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := filepath.Join(t.TempDir(), "singbox.json")
			if err := os.WriteFile(remoteTemplateCachePath(base), []byte(profile), 0o644); err != nil {
				t.Fatal(err)
			}
			root, err := BuildConfigFromRemoteTemplate(core.SingboxSection{UUID: "user-credential", DisabledOutbounds: tc.disabled}, base, time.Nanosecond, nil)
			if tc.auto == nil {
				if err == nil {
					t.Fatal("all disabled: expected an error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := root.Get("outbounds")
			byTag := map[string]*OrderedMap{}
			for _, item := range raw.([]any) {
				ob := item.(*OrderedMap)
				tag, _ := ob.Get("tag")
				byTag[tag.(string)] = ob
			}
			for _, tag := range []string{"auto", "pick", "direct", "reject", "dns-out"} {
				if byTag[tag] == nil {
					t.Fatalf("lost plumbing outbound %q", tag)
				}
			}
			if got, _ := byTag["auto"].Get("outbounds"); !reflect.DeepEqual(got, tc.auto) {
				t.Errorf("auto = %v, want %v", got, tc.auto)
			}
			if len(tc.disabled) == 0 {
				if byTag["transport"] == nil {
					t.Fatal("lost transport dependency")
				}
				if got, _ := byTag["transport"].Get("password"); got != "server-owned" {
					t.Errorf("transport credential overwritten: %v", got)
				}
				if got, _ := byTag["regional"].Get("outbounds"); !reflect.DeepEqual(got, []any{"a", "b"}) {
					t.Errorf("regional candidates flattened: %v", got)
				}
				if got, _ := byTag["a"].Get("uuid"); got != "user-credential" {
					t.Errorf("proxy credential = %v", got)
				}
			} else {
				if byTag["transport"] != nil {
					t.Error("unused transport helper retained")
				}
				if byTag["regional"] != nil || byTag["a"] != nil || byTag["b"] != nil {
					t.Error("disabled proxies or empty group retained")
				}
				if got, _ := byTag["pick"].Get("outbounds"); !reflect.DeepEqual(got, []any{"c"}) {
					t.Errorf("selector candidates = %v", got)
				}
				if got, _ := byTag["pick"].Get("default"); got != "c" {
					t.Errorf("selector default = %v", got)
				}
			}
		})
	}
}

// The profile that started the tunnel an hour ago is still good: a blip on the
// update host must not be the reason the VPN will not come up.
func TestFetchTemplateFallsBackToCache(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "singbox_client.json")
	profile := []byte(`{"outbounds":[{"type":"vless","tag":"a","uuid":""},{"type":"urltest","tag":"auto","outbounds":["a"]}],"route":{"final":"auto"}}`)
	if err := os.WriteFile(cache, profile, 0o644); err != nil {
		t.Fatal(err)
	}

	var warned string
	// A 1ns timeout stands in for an unreachable server.
	body, err := fetchTemplateOrCache(cache, time.Nanosecond, func(m string) { warned = m })
	if err != nil {
		t.Fatalf("download failed and the cache was not used: %v", err)
	}
	if string(body) != string(profile) {
		t.Errorf("got %q, want the cached profile", body)
	}
	if !strings.Contains(warned, cache) {
		t.Errorf("the fallback was silent: %q", warned)
	}
}

// Without a cache there is nothing to fall back to, and the error has to say
// both what failed and where the cache was looked for.
func TestFetchTemplateWithoutCacheReports(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "singbox_client.json")
	_, err := fetchTemplateOrCache(cache, time.Nanosecond, nil)
	if err == nil {
		t.Fatal("expected an error with no download and no cache")
	}
	if !strings.Contains(err.Error(), cache) {
		t.Errorf("error does not name the cache path: %v", err)
	}
}

// Junk from the server must not overwrite a good cache.
func TestFetchTemplateKeepsCacheOnGarbage(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "singbox_client.json")
	good := []byte(`{"outbounds":[{"type":"vless","tag":"a","uuid":""},{"type":"urltest","tag":"auto","outbounds":["a"]}],"route":{"final":"auto"}}`)
	if err := os.WriteFile(cache, good, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := fetchTemplateOrCache(cache, time.Nanosecond, nil); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(cache)
	if err != nil || !json.Valid(after) || string(after) != string(good) {
		t.Errorf("cache after a failed download = %q", after)
	}
}

func TestRemoteTemplateCachePathSitsNextToConfig(t *testing.T) {
	got := remoteTemplateCachePath("/opt/old/cfg/singbox.json")
	want := "/opt/old/cfg/singbox_client.json"
	if got != want {
		t.Errorf("cache path = %q, want %q", got, want)
	}
}

// The macOS client takes the profile whole too: a section it has never heard of
// reaches sing-box, and the local base only fills in what the profile omits.
func TestBuildConfigFromRemoteTemplateTakesTheWholeProfile(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "singbox.json")
	if err := os.WriteFile(base, []byte(`{"log":{"level":"warn"},"inbounds":[{"type":"tun","tag":"tun-in"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := `{"outbounds":[{"type":"vless","tag":"vless-reality","uuid":""},{"type":"urltest","tag":"auto","outbounds":["vless-reality"]},{"type":"direct","tag":"direct"}],` +
		`"ntp":{"enabled":true},"services":[{"type":"ssm-api","tag":"ssm"}],"log":{"level":"debug"}}`
	if err := os.WriteFile(filepath.Join(dir, "singbox_client.json"), []byte(profile), 0o644); err != nil {
		t.Fatal(err)
	}

	// A 1ns timeout stands in for an unreachable server; the cache above is used.
	root, err := BuildConfigFromRemoteTemplate(core.SingboxSection{UUID: "11111111-1111-1111-1111-111111111111", EnabledOutbounds: []string{"vless-reality"}}, base, time.Nanosecond, nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"ntp", "services"} {
		if !root.Has(key) {
			t.Errorf("%s was dropped", key)
		}
	}
	log, _ := root.Get("log")
	if lvl, _ := log.(*OrderedMap).Get("level"); lvl != "debug" {
		t.Errorf("log level = %v, want the profile's", lvl)
	}
	if !root.Has("inbounds") {
		t.Error("the local inbounds were lost")
	}
}

// A log path out of a profile is a path on the machine that wrote it, and a
// relative one resolves against whatever directory sing-box was started in.
func TestNormalizeLogOutput(t *testing.T) {
	for _, tc := range []struct {
		name, output, want string
	}{
		{"relative under the usual prefix", "logs/client.log", "/Library/Logs/JorbiqueVPN/client.log"},
		{"a bare file name", "sing-box.log", "/Library/Logs/JorbiqueVPN/sing-box.log"},
		{"someone else's disk", "C:/Users/Someone/AppData/Roaming/JorbiqueVPN/logs/x.log", "/Library/Logs/JorbiqueVPN/x.log"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := NewOrderedMap()
			log := NewOrderedMap()
			log.Set("level", "warn")
			log.Set("output", tc.output)
			root.Set("log", log)

			NormalizeLogOutput(root, "/Library/Logs/JorbiqueVPN")

			if got, _ := log.Get("output"); got != tc.want {
				t.Errorf("output = %v, want %q", got, tc.want)
			}
		})
	}
}

// A profile that says nothing about the log keeps saying nothing: sing-box then
// writes to stdout, which is where launchd already collects it.
func TestNormalizeLogOutputLeavesASilentProfileAlone(t *testing.T) {
	root := NewOrderedMap()
	log := NewOrderedMap()
	log.Set("level", "warn")
	root.Set("log", log)

	NormalizeLogOutput(root, "/Library/Logs/JorbiqueVPN")

	if log.Has("output") {
		t.Error("an output was invented for a profile that did not ask for one")
	}
}

// The client reads nothing from a running core, so whichever side brings sing-box's
// local API — an old local config or the profile — it does not reach the tunnel.
func TestBuildConfigFromRemoteTemplateRemovesLocalAPI(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "singbox.json")
	if err := os.WriteFile(base, []byte(`{"log":{"level":"warn"},"inbounds":[],"services":[{"type":"api","secret":"stable-secret"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := `{"outbounds":[{"type":"vless","tag":"vless-reality","uuid":""},{"type":"urltest","tag":"auto","outbounds":["vless-reality"]},{"type":"direct","tag":"direct"}],` +
		`"http_clients":[{"tag":"dashboard-http"}]}`
	if err := os.WriteFile(filepath.Join(dir, "singbox_client.json"), []byte(profile), 0o644); err != nil {
		t.Fatal(err)
	}

	// A 1ns timeout stands in for an unreachable server; the cache above is used.
	root, err := BuildConfigFromRemoteTemplate(core.SingboxSection{UUID: "11111111-1111-1111-1111-111111111111", EnabledOutbounds: []string{"vless-reality"}}, base, time.Nanosecond, nil)
	if err != nil {
		t.Fatal(err)
	}
	if root.Has("services") || root.Has("http_clients") {
		t.Errorf("local API remains in config: services=%v http_clients=%v", root.Has("services"), root.Has("http_clients"))
	}
}

// The macOS build reads the profile's own route, DNS and rule sets: a
// transport one of them names outlives the proxies detouring through it, and a
// transport a routed group offers is the user's to turn off.
func TestRemoteTemplateReadsTheProfilesRoute(t *testing.T) {
	const leaves = `
		{"type":"vless","tag":"a","uuid":"","detour":"transport"},
		{"type":"tuic","tag":"b","uuid":""},
		{"type":"shadowtls","tag":"transport","password":"server-owned"},
		{"type":"urltest","tag":"auto","outbounds":["a","b"]},
		{"type":"direct","tag":"direct"}`
	build := func(group, extra string, disabled []string) map[string]*OrderedMap {
		t.Helper()
		base := filepath.Join(t.TempDir(), "singbox.json")
		if err := os.WriteFile(remoteTemplateCachePath(base), []byte(`{"outbounds":[`+leaves+group+`],`+extra+`}`), 0o644); err != nil {
			t.Fatal(err)
		}
		root, err := BuildConfigFromRemoteTemplate(core.SingboxSection{UUID: "user-credential", DisabledOutbounds: disabled}, base, time.Nanosecond, nil)
		if err != nil {
			t.Fatalf("%s: %v", extra, err)
		}
		raw, _ := root.Get("outbounds")
		index := map[string]*OrderedMap{}
		for _, item := range raw.([]any) {
			ob := item.(*OrderedMap)
			index[ob.GetString("tag")] = ob
		}
		return index
	}
	for _, extra := range []string{
		`"dns":{"servers":[{"tag":"remote","type":"udp","server":"1.1.1.1","detour":"transport"}]},"route":{"final":"auto"}`,
		`"route":{"final":"auto","rules":[{"type":"logical","mode":"or","rules":[{"domain":["x"]}],"outbound":"transport"}]}`,
		`"route":{"final":"auto","rule_set":[{"type":"remote","tag":"ads","format":"binary","url":"https://example.com/ads.srs","download_detour":"transport"}]}`,
	} {
		transport := build("", extra, []string{"a"})["transport"]
		if transport == nil || transport.GetString("password") != "server-owned" {
			t.Errorf("%s: transport = %v, want it kept with its own secret", extra, transport)
		}
	}
	got, _ := build(`,{"type":"selector","tag":"proxy","outbounds":["auto","transport"]}`, `"route":{"final":"proxy"}`, []string{"a", "transport"})["proxy"].Get("outbounds")
	if !reflect.DeepEqual(got, []any{"auto"}) {
		t.Errorf("proxy = %v, want the transport the user turned off gone", got)
	}
}

// A transport kept only for a resolver, a rule set or a group nothing routes
// to is no tunnel, and a route rule naming it is one.
func TestRemoteTemplateATransportNothingRoutesToIsNoTunnel(t *testing.T) {
	const leaves = `
		{"type":"vless","tag":"a","uuid":"","detour":"transport"},
		{"type":"tuic","tag":"b","uuid":""},
		{"type":"shadowtls","tag":"transport","password":"server-owned"},
		{"type":"urltest","tag":"auto","outbounds":["a","b"]},
		{"type":"direct","tag":"direct"}`
	build := func(extra string) error {
		t.Helper()
		base := filepath.Join(t.TempDir(), "singbox.json")
		if err := os.WriteFile(remoteTemplateCachePath(base), []byte(`{"outbounds":[`+leaves+extra+`}`), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := BuildConfigFromRemoteTemplate(core.SingboxSection{UUID: "user-credential", DisabledOutbounds: []string{"a", "b"}}, base, time.Nanosecond, nil)
		return err
	}
	for _, extra := range []string{
		`],"dns":{"servers":[{"tag":"remote","type":"udp","server":"1.1.1.1","detour":"transport"}]},"route":{"final":"auto"}`,
		`],"route":{"final":"auto","rule_set":[{"type":"remote","tag":"ads","format":"binary","url":"https://example.com/ads.srs","download_detour":"transport"}]}`,
		`,{"type":"selector","tag":"leftover","outbounds":["transport"]}],"route":{"final":"auto"}`,
	} {
		if err := build(extra); err == nil || !strings.Contains(err.Error(), "enable at least one protocol") {
			t.Errorf("%s: err = %v, want nothing to connect through", extra, err)
		}
	}
	if err := build(`],"route":{"final":"auto","rules":[{"domain":["x"],"outbound":"transport"}]}`); err != nil {
		t.Errorf("a route rule to the transport: %v", err)
	}
}

// The UUID is the credential the protocols authenticate with: a protocol left
// on without one is refused, and with every protocol off there is nothing to
// connect through at all.
func TestBuildConfigFromRemoteTemplateWithoutAUUID(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "singbox.json")
	if err := os.WriteFile(base, []byte(`{"log":{"level":"warn"},"inbounds":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := `{"outbounds":[{"type":"vless","tag":"vless-reality","uuid":""},{"type":"urltest","tag":"auto","outbounds":["vless-reality"]},{"type":"direct","tag":"direct"}]}`
	if err := os.WriteFile(filepath.Join(dir, "singbox_client.json"), []byte(profile), 0o644); err != nil {
		t.Fatal(err)
	}

	// A 1ns timeout stands in for an unreachable server; the cache above is used.
	if _, err := BuildConfigFromRemoteTemplate(core.SingboxSection{}, base, time.Nanosecond, nil); err == nil || !strings.Contains(err.Error(), "uuid is empty") {
		t.Errorf("err = %v, want the missing UUID refused", err)
	}
	if _, err := BuildConfigFromRemoteTemplate(core.SingboxSection{DisabledOutbounds: []string{"vless-reality"}}, base, time.Nanosecond, nil); err == nil || !strings.Contains(err.Error(), "enable at least one protocol") {
		t.Errorf("err = %v, want every protocol off refused", err)
	}
}

// The cache is held to the rule a download is: a profile with nothing to
// connect through, which an older client cached, is no fallback.
func TestFetchTemplateRefusesAnUnbuildableCache(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "singbox_client.json")
	if err := os.WriteFile(cache, []byte(`{"outbounds":[{"type":"direct","tag":"direct"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := fetchTemplateOrCache(cache, time.Nanosecond, nil); err == nil {
		t.Fatal("a cached profile with no proxy outbounds was used")
	}
}

func TestProfileProblem(t *testing.T) {
	for body, bad := range map[string]bool{
		`not json`:         true,
		`{"outbounds":[]}`: true,
		`{"outbounds":[{"type":"vless","tag":"a","uuid":"","detour":"gone"}]}`:                                                            true,
		`{"outbounds":[{"type":"vless","tag":"a","uuid":""},{"type":"urltest","tag":"auto","outbounds":["a"]}],"route":{"final":"auto"}}`: false,
	} {
		if got := profileProblem([]byte(body)) != ""; got != bad {
			t.Errorf("%s: problem = %v, want %v", body, got, bad)
		}
	}
}
