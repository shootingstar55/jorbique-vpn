package semver

import "testing"

func TestCompare_PrereleaseAndLengths(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.3", 0},
		{"1.2.3-rc1", "1.2.3", -1},  // a pre-release is older than the release
		{"1.2.3+build", "1.2.3", 0}, // build metadata stripped
		{"1.2", "1.2.0", 0},         // differing component counts pad with zeros
		{"1.13.7", "1.13.10", -1},
		{"2.0.0", "1.9.9", 1},
		{"1.0.0", "1.0.1", -1},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestParse_NonNumericComponent(t *testing.T) {
	// A non-numeric component is treated as 0, so "1.x.3" == "1.0.3".
	if Compare("1.x.3", "1.0.3") != 0 {
		t.Error("non-numeric component should parse as 0")
	}
	// Empty strings compare equal (both parse to nil).
	if Compare("", "") != 0 {
		t.Error("empty versions should compare equal")
	}
}
