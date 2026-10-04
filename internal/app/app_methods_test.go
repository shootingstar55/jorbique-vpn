package app

import (
	"jorbiquevpn/internal/config"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"jorbiquevpn/internal/constants"
	"jorbiquevpn/internal/server"
)

const appTestUUID = "11111111-1111-1111-1111-111111111111"

func newTestApp(t *testing.T) *App {
	t.Helper()
	isolateProfileCache(t)
	return &App{dataDir: t.TempDir(), store: &server.Store{}}
}

// isolateProfileCache points the downloaded sing-box profile at a directory of
// this test's own.
//
// config.RemoteSingboxCachePath lives under os.TempDir(), which APPDATA does
// not redirect — so a test that says "no profile has been downloaded" was
// really saying "whatever this machine has cached", and read the developer's
// own profile the moment they had run the client. It cuts both ways: anything
// that constructs an app fetches the profile in the background, so an
// unisolated test writes that cache and every later test in the run inherits
// it.
func isolateProfileCache(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir) // darwin, linux
	t.Setenv("TMP", dir)    // windows
	t.Setenv("TEMP", dir)
}

// The tags a profile served while these tests were written. They are a fixture
// now, not a list the client believes in: what exists is whatever the
// downloaded profile says.
var testProfileTags = []string{"vless-reality", "vless-grpc", "tuic", "hysteria2", "anytls"}

func allOutboundsEnabled() map[string]bool {
	m := make(map[string]bool, len(testProfileTags))
	for _, tag := range testProfileTags {
		m[tag] = true
	}
	return m
}

func TestApp_GetDataDirAndIsConnected(t *testing.T) {
	a := newTestApp(t)
	if a.GetDataDir() != a.dataDir {
		t.Error("GetDataDir mismatch")
	}
	if a.IsConnected() { // core.IsRunning is false on the stub platform
		t.Error("IsConnected should be false")
	}
}

func TestApp_GetCurrentStackMode(t *testing.T) {
	a := newTestApp(t)
	profile := filepath.Join(a.dataDir, ".current_profile")

	if a.GetCurrentStackMode() != "" {
		t.Error("missing profile should yield empty")
	}
	cases := map[string]string{
		"Tun System": "System",
		"tun gvisor": "Gvisor",
		"MIXED":      "Mixed",
		"custom":     "Custom",
	}
	for content, want := range cases {
		if err := os.WriteFile(profile, []byte(content+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if got := a.GetCurrentStackMode(); got != want {
			t.Errorf("profile %q => %q, want %q", content, got, want)
		}
	}
}

func TestApp_SetAndGetVpnUserConfig(t *testing.T) {
	a := newTestApp(t)
	msg, err := a.SetVpnUserConfig(appTestUUID, allOutboundsEnabled())
	if err != nil {
		t.Fatalf("SetVpnUserConfig failed: %s (%v)", msg, err)
	}
	// Direct domains are saved on their own now — they belong to the ignore
	// window, next to the process exceptions they are a kind of.
	if msg, err := a.SetDirectDomains("example.com\ncorp.local"); err != nil {
		t.Fatalf("SetDirectDomains failed: %s (%v)", msg, err)
	}
	if !a.HasVpnUserConfig() {
		t.Error("HasVpnUserConfig should be true after a valid save")
	}
	form := a.GetVpnUserConfig()
	if form.UUID != appTestUUID {
		t.Errorf("UUID = %q", form.UUID)
	}
	if !strings.Contains(form.DirectDomains, "example.com") {
		t.Errorf("direct domains = %q", form.DirectDomains)
	}

	// servers.json is persisted and reloadable.
	reloaded, err := server.Load(a.dataDir)
	if err != nil || reloaded.GetUserUUID() != appTestUUID {
		t.Errorf("persisted store mismatch: %v / %q", err, reloaded.GetUserUUID())
	}
}

func TestApp_SetVpnUserConfig_Validation(t *testing.T) {
	a := newTestApp(t)
	// No UUID.
	if _, err := a.SetVpnUserConfig("", allOutboundsEnabled()); err == nil {
		t.Error("empty config should error")
	}
	// Invalid UUID.
	if _, err := a.SetVpnUserConfig("not-a-uuid", allOutboundsEnabled()); err == nil {
		t.Error("invalid UUID should error")
	}
	// Every outbound explicitly disabled used to be refused here. It is refused
	// where it can be judged — on connect, against the profile — because this
	// check has no profile in a settings window that has never been online, and
	// a saved "all off" is a state the user is allowed to be in while editing.
	if _, err := a.SetVpnUserConfig(appTestUUID, map[string]bool{"tuic": false}); err != nil {
		t.Errorf("saving a partial choice should be allowed: %v", err)
	}
	// Domains are saved without a UUID at all: the ignore window knows nothing
	// about the account.
	if msg, err := a.SetDirectDomains("example.com"); err != nil {
		t.Errorf("saving domains alone should succeed: %s %v", msg, err)
	}
	if got := a.GetDirectDomains(); !strings.Contains(got, "example.com") {
		t.Errorf("direct domains = %q", got)
	}
}

// With no profile cached the window draws no toggles and sends an empty map;
// saving the UUID from there must not forget what the user turned off.
func TestApp_SetVpnUserConfigKeepsChoicesWithoutAProfile(t *testing.T) {
	a := newTestApp(t)
	a.store.SetUserUUIDAndOutbounds(appTestUUID, map[string]bool{"tuic": false, "anytls": true})
	if _, err := a.SetVpnUserConfig(appTestUUID, map[string]bool{"anytls": false}); err != nil {
		t.Fatal(err)
	}
	if got, want := a.store.OutboundEnabledFor(nil), map[string]bool{"tuic": false, "anytls": false}; !reflect.DeepEqual(got, want) {
		t.Errorf("stored choices = %v, want %v", got, want)
	}
}

func TestApp_VpnFormDraftRoundTrip(t *testing.T) {
	a := newTestApp(t)
	if d, err := readVpnFormDraft(a.dataDir); err != nil || d != nil {
		t.Errorf("missing draft should be (nil,nil): %v %v", d, err)
	}
	if err := a.SaveVpnFormDraft(appTestUUID, "draft.example", map[string]bool{"tuic": false}); err != nil {
		t.Fatal(err)
	}
	d, err := readVpnFormDraft(a.dataDir)
	if err != nil || d == nil || d.UUID != appTestUUID {
		t.Fatalf("draft not read back: %v %+v", err, d)
	}

	// A draft newer than the last save wins in GetVpnUserConfig / HasVpnUserConfig.
	a.store.SetConfigSavedAt(d.UpdatedAt - 10)
	form := a.GetVpnUserConfig()
	if form.UUID != appTestUUID || form.DirectDomains != "draft.example" {
		t.Errorf("draft did not override store: %+v", form)
	}
	if !a.HasVpnUserConfig() {
		t.Error("HasVpnUserConfig should honor a newer draft with a valid UUID")
	}

	removeVpnFormDraft(a.dataDir)
	if d, _ := readVpnFormDraft(a.dataDir); d != nil {
		t.Error("draft not removed")
	}
}

func TestApp_EffectiveVpnUserForConnect(t *testing.T) {
	a := newTestApp(t)
	a.store.SetUserUUIDAndOutbounds(appTestUUID, allOutboundsEnabled())
	a.store.SetDirectDomains([]string{"store.example"})
	a.store.SetConfigSavedAt(100)

	// No draft: store values are returned.
	uuid, en, dd := a.effectiveVpnUserForConnect()
	if uuid != appTestUUID || len(dd) != 1 || dd[0] != "store.example" {
		t.Errorf("store path: %q %v", uuid, dd)
	}
	if !en["tuic"] {
		t.Error("store outbounds expected enabled")
	}

	// Newer draft overrides.
	_ = a.SaveVpnFormDraft("22222222-2222-2222-2222-222222222222", "draft.example", map[string]bool{"tuic": false})
	uuid, en, dd = a.effectiveVpnUserForConnect()
	if uuid != "22222222-2222-2222-2222-222222222222" {
		t.Errorf("draft UUID not used: %q", uuid)
	}
	if dd[0] != "draft.example" {
		t.Errorf("draft direct domains not used: %v", dd)
	}
	if en["tuic"] {
		t.Error("draft outbound toggle not applied")
	}
}

func TestApp_LogFileOperations(t *testing.T) {
	a := newTestApp(t)
	logsDir := filepath.Join(a.dataDir, "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logsDir, "controller.log"), []byte("line1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	content, err := a.GetLogContent("controller.log")
	if err != nil || !strings.Contains(content, "line1") {
		t.Errorf("GetLogContent = %q %v", content, err)
	}
	// Default name when empty.
	if _, err := a.GetLogContent(""); err != nil {
		t.Errorf("default log name failed: %v", err)
	}
	// Path traversal rejected.
	if _, err := a.GetLogContent("../secret"); err == nil {
		t.Error("path traversal should be rejected")
	}

	names, err := a.ListLogFiles()
	if err != nil || len(names) == 0 {
		t.Errorf("ListLogFiles = %v %v", names, err)
	}

	if err := a.ClearLogs(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(logsDir, "controller.log")); err == nil {
		t.Error("ClearLogs should remove log files")
	}
}

func TestApp_IgnoredProcessesAndPaths(t *testing.T) {
	a := newTestApp(t)
	if err := a.SetIgnoredProcessNamesAndPaths([]string{"Chrome", "chrome", "  ", "Game"}, nil); err != nil {
		t.Fatal(err)
	}
	names := a.GetIgnoredProcessNames()
	if len(names) != 2 { // de-duplicated case-insensitively
		t.Errorf("expected 2 unique names, got %v", names)
	}

	var p string
	if runtime.GOOS == "windows" {
		p = `C:\Program Files\app.exe`
	} else {
		p = "/usr/bin/app"
	}
	if err := a.SetIgnoredProcessNamesAndPaths([]string{"x"}, []string{p, p}); err != nil {
		t.Fatalf("set paths failed: %v", err)
	}
	if got := a.GetIgnoredProcessPaths(); len(got) != 1 {
		t.Errorf("expected 1 deduped path, got %v", got)
	}

	// Relative paths are rejected.
	if err := a.SetIgnoredProcessNamesAndPaths(nil, []string{"relative/path"}); err == nil {
		t.Error("relative path should be rejected")
	}
}

func TestApp_SingboxInstallAndChannel(t *testing.T) {
	a := newTestApp(t)
	if a.IsSingboxInstalled() {
		t.Error("should not be installed in an empty data dir")
	}
	if err := os.WriteFile(filepath.Join(a.dataDir, constants.ProcessExeName), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if !a.IsSingboxInstalled() {
		t.Error("should detect installed sing-box exe")
	}
}

func TestApp_ConnectSavedStack_OpenUICases(t *testing.T) {
	a := newTestApp(t)
	// No VPN config -> openUI true, no connect attempt.
	if openUI, err := a.ConnectSavedStack(); !openUI || err != nil {
		t.Errorf("no config: openUI=%v err=%v", openUI, err)
	}

	// Valid config but no preferred stack -> openUI true.
	a.store.SetUserUUIDAndOutbounds(appTestUUID, allOutboundsEnabled())
	a.store.SetConfigSavedAt(time.Now().Unix())
	if openUI, err := a.ConnectSavedStack(); !openUI || err != nil {
		t.Errorf("no stack: openUI=%v err=%v", openUI, err)
	}
}

func TestApp_StaleArtifactsAndCacheRemoval(t *testing.T) {
	a := newTestApp(t)
	if a.staleTunnelArtifactsPresent(a.dataDir) {
		t.Error("fresh data dir has no stale artifacts")
	}
	// A .current_profile marks a stale session.
	if err := os.WriteFile(filepath.Join(a.dataDir, ".current_profile"), []byte("system"), 0644); err != nil {
		t.Fatal(err)
	}
	if !a.staleTunnelArtifactsPresent(a.dataDir) {
		t.Error("current_profile should count as a stale artifact")
	}

	// removeConfigCache is a no-op when the cache dir is absent, removes it otherwise.
	removeConfigCache(a.dataDir, nil)
	cacheDir := filepath.Join(a.dataDir, "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatal(err)
	}
	removeConfigCache(a.dataDir, func(string, string) {})
	if _, err := os.Stat(cacheDir); err == nil {
		t.Error("cache dir should be removed")
	}
}

func TestNormalizeBypassPaths(t *testing.T) {
	var abs string
	if runtime.GOOS == "windows" {
		abs = `C:\Apps\foo.exe`
	} else {
		abs = "/opt/foo"
	}
	out, err := normalizeBypassPaths([]string{abs, abs, "  "})
	if err != nil || len(out) != 1 {
		t.Fatalf("dedup/clean failed: %v %v", out, err)
	}
	// Relative path rejected.
	if _, err := normalizeBypassPaths([]string{"relative"}); err == nil {
		t.Error("relative path should error")
	}
	// Empty input yields an empty result.
	if out, err := normalizeBypassPaths(nil); err != nil || len(out) != 0 {
		t.Errorf("nil input: %v %v", out, err)
	}
}

// With no profile downloaded there is nothing to fill a map in from, so what
// the caller passed is what gets saved — losing a user's choices because the
// network was down would be the worse answer.
func TestNormalizeOutboundEnabledForSaveWithoutAProfile(t *testing.T) {
	out := normalizeOutboundEnabledForSave(map[string]bool{"tuic": false, "anytls": true}, nil)
	if out["tuic"] || !out["anytls"] {
		t.Errorf("choices not preserved: %v", out)
	}
	if got := normalizeOutboundEnabledForSave(nil, nil); len(got) != 0 {
		t.Errorf("nothing in, nothing out, got %v", got)
	}
}

func TestNormalizeOutboundEnabledForSaveUsesAvailableSnapshot(t *testing.T) {
	in := map[string]bool{"tuic": false, "removed": true}
	out := normalizeOutboundEnabledForSave(in, []string{"tuic", "new-protocol"})
	want := map[string]bool{"tuic": false, "new-protocol": true}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("normalized choices = %v, want %v", out, want)
	}
	if !reflect.DeepEqual(in, map[string]bool{"tuic": false, "removed": true}) {
		t.Errorf("input choices were changed: %v", in)
	}
}

// A rename on the server costs nobody their connection, but the log says which
// outbounds went: only ones the user had on, since a refusal that vanished
// changes nothing they would notice.
func TestVanishedOutbounds(t *testing.T) {
	a := newTestApp(t)
	available := []string{"tuic", "new"}
	if got := a.vanishedOutbounds(a.store.OutboundEnabledFor(available), available); len(got) != 0 {
		t.Errorf("nothing stored, nothing vanished: %v", got)
	}
	a.store.SetUserUUIDAndOutbounds(appTestUUID, map[string]bool{"tuic": true, "old-b": true, "old-a": true, "refused": false})
	// What the build is handed has the gone tags filtered out already.
	effective := a.store.OutboundEnabledFor(available)
	if got := a.vanishedOutbounds(effective, available); !reflect.DeepEqual(got, []string{"old-a", "old-b"}) {
		t.Errorf("vanished = %v", got)
	}
	// A newer draft that turned one off answers for it.
	effective["old-a"] = false
	if got := a.vanishedOutbounds(effective, available); !reflect.DeepEqual(got, []string{"old-b"}) {
		t.Errorf("vanished with a draft = %v", got)
	}
	// A profile with no proxy outbounds is not a rename: the saved answers
	// about proxies are not reported as lost.
	if got := a.vanishedOutbounds(a.store.OutboundEnabledFor(nil), nil); len(got) != 0 {
		t.Errorf("a profile without proxies named outbounds as vanished: %v", got)
	}
}

func TestHasSomethingToRouteThrough(t *testing.T) {
	// The profile decides which answers count.
	if !hasSomethingToRouteThrough(map[string]bool{}, testProfileTags) {
		t.Error("no stored answers means every offered outbound is on")
	}
	none := map[string]bool{}
	for _, tag := range testProfileTags {
		none[tag] = false
	}
	if hasSomethingToRouteThrough(none, testProfileTags) {
		t.Error("all-disabled map should report false")
	}

	// A protocol the client has never heard of counts, because the profile
	// lists it and nothing has been said against it.
	if !hasSomethingToRouteThrough(map[string]bool{}, []string{"shadowtls-fi"}) {
		t.Error("a protocol added on the server does not count")
	}
	// Answers about outbounds the profile no longer offers decide nothing: the
	// five it does offer have no answer, so they are on.
	if !hasSomethingToRouteThrough(map[string]bool{"gone": false}, testProfileTags) {
		t.Error("a stale answer about a dropped outbound turned everything off")
	}

	// No profile: nothing to judge against, and the connect path will check
	// again with one in hand rather than blocking the settings window now.
	if !hasSomethingToRouteThrough(map[string]bool{"tuic": false}, nil) {
		t.Error("the settings window was blocked on a guess")
	}
}

func TestApp_RecoverFromUnexpectedExit_NoArtifacts(t *testing.T) {
	a := newTestApp(t)
	// Fresh data dir: no stale artifacts, so it returns without touching routes.
	if msg, err := a.RecoverFromUnexpectedExit(); err != nil || msg != "" {
		t.Errorf("recover = %q %v", msg, err)
	}
}

func TestApp_ListActiveApps(t *testing.T) {
	a := newTestApp(t)
	procs, err := a.ListActiveApps()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	_ = procs // empty on non-Windows; just exercising the delegation
}

func TestWarnConfigInitAndControllerLog(t *testing.T) {
	warnConfigInit(nil)           // nil error: no-op
	warnConfigInit(os.ErrInvalid) // logs a warning
	if controllerLog(t.TempDir(), true) == nil {
		t.Error("controllerLog should return a logger")
	}
}

// The sing-box cache dir must be removed only at full app shutdown (tray Quit
// calls CleanupSingboxCacheOnExit); on the stub platform core.IsRunning is
// false, so the removal branch runs.
func TestApp_CleanupSingboxCacheOnExit(t *testing.T) {
	a := newTestApp(t)
	cacheDir := filepath.Join(a.dataDir, "cache", "tun_system")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "cache.db"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	a.CleanupSingboxCacheOnExit()

	if _, err := os.Stat(filepath.Join(a.dataDir, "cache")); !os.IsNotExist(err) {
		t.Fatal("cache dir survived full-shutdown cleanup")
	}
}

// A disconnect removes the running-profile file, and the window still has to
// say which stack it is on — the one it will come up on, which is the saved
// preference. It used to show a dash there while macOS showed the stack.
func TestCurrentStackModeFallsBackToTheSavedOne(t *testing.T) {
	a := newTestApp(t)

	if got := a.GetCurrentStackMode(); got != "" {
		t.Errorf("with nothing saved and nothing running the stack is %q, want empty", got)
	}

	a.store.SetPreferredStackMode("gvisor")
	if got := a.GetCurrentStackMode(); got != "Gvisor" {
		t.Errorf("stack = %q, want the saved Gvisor", got)
	}

	// A running profile still wins: that is the stack actually in use.
	if err := os.WriteFile(filepath.Join(a.GetDataDir(), ".current_profile"), []byte("tun_mixed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := a.GetCurrentStackMode(); got != "Mixed" {
		t.Errorf("stack = %q, want the running Mixed", got)
	}
}

// Saving every protocol off is recorded as said; it is Connect that refuses a
// configuration with nothing to route through.
func TestApp_SetVpnUserConfig_AcceptsEveryProtocolOff(t *testing.T) {
	// newTestApp moves the profile cache aside first: it lives under the system
	// temp directory, not under APPDATA, so a cache left in the real one is a
	// profile every later test in this package would see. The fixture goes in
	// after that, or it would be written where nothing looks for it.
	a := newTestApp(t)
	if err := os.WriteFile(config.RemoteSingboxCachePath(), []byte(`{
	  "outbounds":[{"type":"vless","tag":"vless-reality"},{"type":"tuic","tag":"tuic"}]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	allOff := map[string]bool{"vless-reality": false, "tuic": false}
	if _, err := a.SetVpnUserConfig(appTestUUID, allOff); err != nil {
		t.Fatalf("every protocol off could not be saved: %v", err)
	}
	saved := a.store.GetOutboundEnabled()
	if saved["vless-reality"] || saved["tuic"] {
		t.Errorf("the refusals were not recorded: %v", saved)
	}
}
