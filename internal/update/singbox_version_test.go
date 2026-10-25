package update

import "testing"

func TestShouldUpdateSingbox(t *testing.T) {
	for _, test := range []struct {
		name, local, remote string
		want                bool
	}{
		{"migrate newer upstream alpha", "1.15.0-alpha.12", "v1.14.3-extended-2.7.3", true},
		{"migrate same upstream release", "1.14.3", "1.14.3-extended-2.7.3", true},
		{"stay on extended", "1.14.3-extended-2.7.3", "1.15.0", false},
		{"same extended build", "1.14.3-extended-2.7.3", "v1.14.3-extended-2.7.3", false},
		{"new extended patch", "1.14.3-extended-2.7.3", "1.14.3-extended-2.7.4", true},
		{"numeric extended major", "1.14.3-extended-9.0.0", "1.14.3-extended-10.0.0", true},
		{"older extended release", "1.14.3-extended-2.7.3", "1.14.3-extended-2.7.2", false},
		{"new extended upstream base", "1.14.3-extended-2.7.3", "1.14.4-extended-2.8.0", true},
		{"older extended upstream base", "1.14.3-extended-2.7.3", "1.14.2-extended-2.8.0", false},
		{"upstream alpha upgrade", "1.15.0-alpha.9", "1.15.0-alpha.10", true},
		{"upstream downgrade", "1.15.0-alpha.12", "1.14.3", false},
		{"metadata alone", "1.14.3-extended-2.7.3+one", "1.14.3-extended-2.7.3+two", false},
		{"initial install", "", "1.14.3-extended-2.7.3", true},
		{"unreachable channel", "1.14.3-extended-2.7.3", "", false},
		{"unrecognized installed version", "unknown", "1.14.3-extended-2.7.3", false},
		{"invalid channel version", "1.15.0-alpha.12", "unknown-extended-2.7.3", false},
		{"invalid extended version", "1.15.0-alpha.12", "1.14.3-extended-broken", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ShouldUpdateSingbox(test.local, test.remote); got != test.want {
				t.Fatalf("ShouldUpdateSingbox(%q, %q) = %v, want %v", test.local, test.remote, got, test.want)
			}
		})
	}
}
