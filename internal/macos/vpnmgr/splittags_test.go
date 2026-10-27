package vpnmgr

import (
	"slices"
	"testing"
)

func TestSplitTagListKeepsATagWithASpace(t *testing.T) {
	available := []string{"eu west", "a", "b"}
	got := splitTagList("eu west, a;b", available)
	if want := []string{"eu west", "a", "b"}; !slices.Equal(got, want) {
		t.Fatalf("splitTagList = %q, want %q", got, want)
	}
	got = splitTagList("a b,,", nil)
	if want := []string{"a", "b"}; !slices.Equal(got, want) {
		t.Fatalf("splitTagList without a profile = %q, want %q", got, want)
	}
}
