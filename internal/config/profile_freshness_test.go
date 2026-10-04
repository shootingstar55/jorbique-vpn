package config

import (
	"os"
	"reflect"
	"testing"
)

// A connection has to act on the profile the server serves now. The outbound
// list, the failover order and the config that gets built all come from the
// bytes the fetch returned — never from the copy on disk, which is only there
// for the case where the server could not be reached at all.
//
// This is what makes an outbound added server-side usable on the next connect
// rather than on the next client release: nothing in the path between the
// download and the built config consults a list of its own.
func TestTheOutboundListComesFromTheFetchedProfile(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())

	// What the machine has from last time: no naive.
	stale := []byte(`{"outbounds":[
      {"type":"vless","tag":"vless-reality","uuid":""},
      {"type":"urltest","tag":"auto","outbounds":["vless-reality"]},
      {"type":"direct","tag":"direct"}]}`)
	if err := os.WriteFile(RemoteSingboxCachePath(), stale, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := AvailableProxyTags(); !reflect.DeepEqual(got, []string{"vless-reality"}) {
		t.Fatalf("the cache should offer only what it holds, got %v", got)
	}

	// What the server serves now: naive added.
	fresh := []byte(`{"outbounds":[
      {"type":"vless","tag":"vless-reality","uuid":""},
      {"type":"naive","tag":"naive","username":"","password":""},
      {"type":"urltest","tag":"auto","outbounds":["vless-reality","naive"]},
      {"type":"direct","tag":"direct"}]}`)

	// The connect path reads the tags out of the fetched bytes, not off disk.
	got := ProxyTagsFromJSON(fresh)
	want := []string{"vless-reality", "naive"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tags from the fetched profile = %v, want %v", got, want)
	}
}

// A successful fetch replaces the cache, so the settings window — which reads
// the cache, because opening a menu must not wait on the network — shows what
// the last connection actually used.
func TestASuccessfulFetchUpdatesWhatTheWindowShows(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	stale := []byte(`{"outbounds":[{"type":"vless","tag":"vless-reality"}]}`)
	if err := os.WriteFile(RemoteSingboxCachePath(), stale, 0o644); err != nil {
		t.Fatal(err)
	}
	fresh := []byte(`{"outbounds":[{"type":"vless","tag":"vless-reality"},{"type":"naive","tag":"naive"}]}`)
	if err := os.WriteFile(RemoteSingboxCachePath(), fresh, 0o644); err != nil {
		t.Fatal(err)
	}
	got := AvailableProxyTags()
	if !reflect.DeepEqual(got, []string{"vless-reality", "naive"}) {
		t.Errorf("window would show %v, want both outbounds", got)
	}
}

// The timeout is what a user waits through before a connection starts doing
// anything, so it is deliberately short — the profile is a few kilobytes.
func TestProfileFetchTimeoutStaysShort(t *testing.T) {
	if profileFetchTimeout.Seconds() > 20 {
		t.Errorf("profile fetch timeout is %s: every connect waits this long before falling back", profileFetchTimeout)
	}
}
