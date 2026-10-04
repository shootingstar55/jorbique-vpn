package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_CorruptJSON_Backup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, serversFileName)
	if err := os.WriteFile(path, []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("expected invalid JSON error, got err=%v store=%v", err, s)
	}
	if s == nil {
		t.Fatal("expected non-nil empty store")
	}
	entries, _ := os.ReadDir(dir)
	var found bool
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "servers.json.corrupt.") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected corrupt backup file")
	}
}
