package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSecurePath_MoreCases(t *testing.T) {
	base := t.TempDir()
	inside := filepath.Join(base, "logs", "app.log")
	if err := os.MkdirAll(filepath.Dir(inside), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inside, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	if !SecurePath(inside, base, true) {
		t.Error("existing path inside base should be allowed")
	}
	if !SecurePath(filepath.Join(base, "new.log"), base, false) {
		t.Error("non-existing path inside base should be allowed when mustExist=false")
	}
	if SecurePath(filepath.Join(base, "missing.log"), base, true) {
		t.Error("mustExist should reject a missing file")
	}
	if SecurePath(filepath.Join(base, "..", "escape.log"), base, false) {
		t.Error("path traversal must be rejected")
	}
	// Base directory that does not exist.
	if SecurePath(inside, filepath.Join(base, "does-not-exist"), false) {
		t.Error("nonexistent base dir should be rejected")
	}
	// A sibling directory outside base.
	outside := t.TempDir()
	if SecurePath(filepath.Join(outside, "x.log"), base, false) {
		t.Error("path outside base should be rejected")
	}
}

func TestValidUUID_RequireV4(t *testing.T) {
	v4 := "11111111-1111-4811-8111-111111111111"
	if !ValidUUID(v4, true) {
		t.Errorf("%q should be a valid v4 UUID", v4)
	}
	// Version nibble not 4.
	if ValidUUID("11111111-1111-1811-8111-111111111111", true) {
		t.Error("non-v4 version nibble should fail requireV4")
	}
	// Second nibble of the version group out of range (not 8/9/a/b).
	if ValidUUID("11111111-1111-4111-8111-111111111111", true) {
		t.Error("invalid version-group nibble should fail requireV4")
	}
	// requireV4=false accepts any well-formed UUID.
	if !ValidUUID("11111111-1111-1111-1111-111111111111", false) {
		t.Error("well-formed UUID should pass without requireV4")
	}
	if ValidUUID("not-a-uuid", false) {
		t.Error("malformed UUID should fail")
	}
}
