package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidUUID(t *testing.T) {
	if !ValidUUID("550e8400-e29b-48d4-8716-446655440000", false) {
		t.Error("Valid UUID should be accepted")
	}
	if ValidUUID("not-a-uuid", false) {
		t.Error("Invalid UUID should be rejected")
	}
	if ValidUUID("550e8400-e29b-41d4-a716", false) {
		t.Error("Short UUID should be rejected")
	}
	if ValidUUID("550e8400-e29b-11d4-a716-446655440000", true) {
		t.Error("Non-v4 UUID should be rejected when RequireV4")
	}
	if !ValidUUID("550e8400-e29b-48d4-8716-446655440000", true) {
		t.Error("Valid v4 UUID should be accepted")
	}
}

func TestSecurePath(t *testing.T) {
	base := os.TempDir()
	file := filepath.Join(base, "test.txt")
	if err := os.WriteFile(file, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(file)

	if !SecurePath(file, base, false) {
		t.Error("Path within base should be valid")
	}
	if SecurePath("C:\\Windows\\System32\\kernel32.dll", base, false) {
		t.Error("Path outside base should be invalid")
	}
}
