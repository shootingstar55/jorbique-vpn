package config

import "testing"

// The naive profile has a cache of its own: sharing the main one would turn the
// protocol list in the settings window into "naive" after a NaiveProxy connect.
func TestNaiveProfileHasItsOwnCache(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if RemoteNaiveCachePath() == RemoteSingboxCachePath() {
		t.Fatal("the naive profile and the main profile share a cache file")
	}
}
