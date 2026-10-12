package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const testUUID = "11111111-1111-1111-1111-111111111111"

func TestStore_UserUUIDAndOutbounds(t *testing.T) {
	// GetOutboundEnabled below reads the downloaded profile, which is cached
	// under os.TempDir() — point that at a directory of this test's own rather
	// than at whatever the machine running the tests has downloaded.
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp) // darwin, linux
	t.Setenv("TMP", tmp)    // windows
	t.Setenv("TEMP", tmp)

	s := &Store{}
	s.SetUserUUIDAndOutbounds("  "+testUUID+"  ", map[string]bool{" tuic ": false, "vless-reality": true})
	if s.GetUserUUID() != testUUID {
		t.Errorf("UUID not trimmed/stored: %q", s.GetUserUUID())
	}
	// What the profile offers is what gets an answer; a tag with none stored is
	// on, which is how a protocol the server adds starts working.
	offered := []string{"vless-reality", "tuic", "hysteria2"}
	en := s.OutboundEnabledFor(offered)
	if en["tuic"] {
		t.Error("tuic should be disabled")
	}
	if !en["vless-reality"] {
		t.Error("vless-reality should be enabled")
	}
	if !en["hysteria2"] {
		t.Error("unset tag should default to enabled")
	}

	// A tag the profile no longer offers is not carried over.
	if _, ok := s.OutboundEnabledFor([]string{"vless-reality"})["tuic"]; ok {
		t.Error("a dropped outbound survived in the answers")
	}
	// With no profile the stored answers are all there is to go on.
	if en := s.OutboundEnabledFor(nil); en["tuic"] || len(en) == 0 {
		t.Errorf("no profile = the stored answers, got %v", en)
	}

	// nil map leaves outbound config untouched.
	s2 := &Store{OutboundEnabled: map[string]bool{"tuic": false}}
	s2.SetUserUUIDAndOutbounds(testUUID, nil)
	if s2.GetOutboundEnabled()["tuic"] {
		t.Error("nil map should not reset stored outbound toggles")
	}
}

func TestStore_ListSettersGetters(t *testing.T) {
	s := &Store{}
	s.SetIgnoredProcessNames([]string{"a", "b"})
	if got := s.GetIgnoredProcessNames(); len(got) != 2 || got[0] != "a" {
		t.Errorf("ignored names = %v", got)
	}
	s.SetIgnoredProcessPaths([]string{"/x", "/y"})
	if got := s.GetIgnoredProcessPaths(); len(got) != 2 {
		t.Errorf("ignored paths = %v", got)
	}
	s.SetDirectDomains([]string{"example.com"})
	if got := s.GetDirectDomains(); len(got) != 1 || got[0] != "example.com" {
		t.Errorf("direct domains = %v", got)
	}

	// Returned slices are copies (mutating them must not affect the store).
	got := s.GetIgnoredProcessNames()
	got[0] = "mutated"
	if s.GetIgnoredProcessNames()[0] == "mutated" {
		t.Error("GetIgnoredProcessNames must return a copy")
	}
}

// Nothing sets this any more — the dashboard switches the node — but a config
// written before that still carries one, and the build must read it back.
func TestStore_SelectedNodeSurvivesAnOlderConfig(t *testing.T) {
	s := &Store{}
	if s.GetSelectedNode() != "" {
		t.Error("nothing picked yet should read as empty, leaving the profile's own default in charge")
	}
	s.SelectedNode = "tuic"
	if got := s.GetSelectedNode(); got != "tuic" {
		t.Errorf("selected node = %q, want tuic", got)
	}
}

func TestStore_ConfigSavedAt(t *testing.T) {
	s := &Store{}
	s.SetConfigSavedAt(42)
	if s.GetConfigSavedAt() != 42 {
		t.Errorf("config saved at = %d", s.GetConfigSavedAt())
	}
}

func TestStore_SaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := &Store{UserUUID: testUUID}
	s.SetDirectDomains([]string{"a.com"})
	s.SetConfigSavedAt(100)
	if err := s.Save(dir); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.GetUserUUID() != testUUID {
		t.Errorf("round trip mismatch: %+v", loaded)
	}
	if len(loaded.GetDirectDomains()) != 1 {
		t.Errorf("direct domains lost: %v", loaded.GetDirectDomains())
	}
}

func TestLoad_MissingAndEmpty(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir) // no file
	if err != nil || s == nil {
		t.Fatalf("missing file should yield empty store: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, serversFileName), []byte("   "), 0644); err != nil {
		t.Fatal(err)
	}
	s, err = Load(dir)
	if err != nil || s == nil || s.GetUserUUID() != "" {
		t.Fatalf("empty file should yield empty store: %v", err)
	}
}

func TestLoad_MigratesLegacyVlessSavedAt(t *testing.T) {
	dir := t.TempDir()
	raw := map[string]interface{}{
		"user_uuid":      testUUID,
		"vless_saved_at": 12345,
	}
	data, _ := json.Marshal(raw)
	if err := os.WriteFile(filepath.Join(dir, serversFileName), data, 0644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.GetConfigSavedAt() != 12345 {
		t.Errorf("legacy vless_saved_at not migrated to config_saved_at: %d", s.GetConfigSavedAt())
	}
}

func TestLoad_SetsConfigSavedAtWhenUUIDPresent(t *testing.T) {
	dir := t.TempDir()
	data, _ := json.Marshal(map[string]interface{}{"user_uuid": testUUID})
	if err := os.WriteFile(filepath.Join(dir, serversFileName), data, 0644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.GetConfigSavedAt() == 0 {
		t.Error("config_saved_at should be backfilled when UUID is present")
	}
}

func TestExtractUUIDFromLegacyURL(t *testing.T) {
	cases := map[string]string{
		"vless reality":  "vless://" + testUUID + "@example.com:443?pbk=key&sni=host&sid=ab",
		"anytls uuid pw": "anytls://" + testUUID + "@example.com:443",
	}
	for name, u := range cases {
		t.Run(name, func(t *testing.T) {
			if got := extractUUIDFromLegacyURL(u); got != testUUID {
				t.Errorf("extractUUIDFromLegacyURL(%q) = %q, want %q", u, got, testUUID)
			}
		})
	}
	if extractUUIDFromLegacyURL("") != "" {
		t.Error("empty URL should yield empty UUID")
	}
	if extractUUIDFromLegacyURL("https://not-a-proxy") != "" {
		t.Error("unrelated URL should yield empty UUID")
	}
}

func TestMigrateLegacyVlessURLsFromRaw(t *testing.T) {
	s := &Store{}
	raw := map[string]interface{}{
		"vless_urls": map[string]interface{}{
			"reality": "vless://" + testUUID + "@h:443?pbk=k&sni=s&sid=ab",
		},
	}
	migrateLegacyVlessURLsFromRaw(s, raw)
	if s.UserUUID != testUUID {
		t.Errorf("UUID not migrated from legacy vless_urls: %q", s.UserUUID)
	}

	// Already has a UUID -> no-op.
	s2 := &Store{UserUUID: "keep"}
	migrateLegacyVlessURLsFromRaw(s2, raw)
	if s2.UserUUID != "keep" {
		t.Error("existing UUID should not be overwritten")
	}
	migrateLegacyVlessURLsFromRaw(&Store{}, nil) // no panic
}
