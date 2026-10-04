package semver

import "testing"

func TestCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.13.4", "1.13.3", 1},
		{"1.13.3", "1.13.4", -1},
		{"1.13.3", "1.13.3", 0},
		{"2.0.0", "1.99.99", 1},
		{"1.0.0", "1.0", 0},
		{"1.14.0-alpha.2", "1.14.0", -1}, // a release is newer than its pre-releases
		{"1.14.0+custom.12", "1.14.0", 0},
		{"1.14.1", "1.14.0-alpha.2", 1},
		{"1.15.0-alpha.10", "1.14.2", 1},
		{"1.15.0-alpha.10", "1.15.0-alpha.9", 1}, // numerically, not lexically
		{"1.15.0", "1.15.0-alpha.10", 1},
		{"1.15.0-beta.1", "1.15.0-alpha.10", 1},
		{"1.15.0-rc.1", "1.15.0-beta.3", 1},
		{"1.15.0-alpha", "1.15.0-alpha.1", -1},
		{"v1.15.0-alpha.10", "1.15.0-alpha.10", 0},
		{"", "1.0.0", -1},
	}
	for _, tc := range tests {
		if got := Compare(tc.a, tc.b); got != tc.want {
			t.Errorf("Compare(%q,%q)=%d want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
