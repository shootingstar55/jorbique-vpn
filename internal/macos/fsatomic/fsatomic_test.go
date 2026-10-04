package fsatomic

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileCreatesParentsAndAppliesMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "file")
	if err := WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Errorf("content = %q", data)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
}

// The mode asked for wins over the one already on disk: a file left readable
// only by its owner is one launchd or mDNSResponder will not read.
func TestWriteFileReplacesModeAndContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, []byte("new"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "new" {
		t.Errorf("content = %q", data)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644", fi.Mode().Perm())
	}
	assertNoTemporaries(t, dir)
}

// The cleanup path is the reason this is shared rather than copied: a failed
// rename must not leave a half-written file in a directory something else is
// watching.
func TestWriteFileCleansUpAfterAFailedRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "occupied")
	if err := os.MkdirAll(filepath.Join(path, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, []byte("x"), 0o644); err == nil {
		t.Error("writing over a non-empty directory must fail")
	}
	assertNoTemporaries(t, dir)
}

func assertNoTemporaries(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp.") {
			t.Errorf("left %s behind", e.Name())
		}
	}
}
