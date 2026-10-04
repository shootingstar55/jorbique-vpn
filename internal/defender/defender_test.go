package defender

import "testing"

// Off Windows AddExclusions is a no-op, and on Windows without Defender or without an
// elevated token every WMI step fails. Both are the same promise to the caller: it never
// panics and it never reports anything.
func TestAddExclusions_NoPanic(t *testing.T) {
	AddExclusions(t.TempDir())
}
