package utils

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetProjectRoot_FindsMarker(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "singbox_cfg.yml"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	got := GetProjectRoot()
	// macOS tmp dirs are symlinks (/var -> /private/var); compare resolved paths.
	gotResolved, _ := filepath.EvalSymlinks(got)
	wantResolved, _ := filepath.EvalSymlinks(dir)
	if gotResolved != wantResolved {
		t.Errorf("GetProjectRoot = %q, want %q", gotResolved, wantResolved)
	}
}

func TestGetProjectRoot_FallbackWhenNoMarker(t *testing.T) {
	// A directory with no marker anywhere up the tree falls back to cwd.
	dir := t.TempDir()
	t.Chdir(dir)
	if GetProjectRoot() == "" {
		t.Error("GetProjectRoot should fall back to a non-empty path")
	}
}

func TestListActiveApps_NonWindowsEmpty(t *testing.T) {
	procs, err := ListActiveApps()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(procs) != 0 {
		t.Errorf("non-Windows ListActiveApps should be empty, got %v", procs)
	}
}

func TestHideCommandWindow_NoPanic(t *testing.T) {
	cmd := exec.Command("true")
	HideCommandWindow(cmd) // no-op on non-Windows; must not panic
}

func TestRelocatePath(t *testing.T) {
	for _, tc := range []struct {
		name, in, prefix, dir, want string
	}{
		{"keeps the shape under the prefix", "cache/tun/cache.db", "cache/", "/data/cache", "/data/cache/tun/cache.db"},
		{"a bare name lands in the directory", "app.log", "logs/", "/data/logs", "/data/logs/app.log"},
		{"a foreign absolute path keeps only its file", `C:\Users\Someone\logs\x.log`, "logs/", "/data/logs", "/data/logs/x.log"},
		{"an empty path is the directory itself", "  ", "logs/", "/data/logs", "/data/logs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := RelocatePath(tc.in, tc.prefix, tc.dir); got != tc.want {
				t.Errorf("RelocatePath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRelocatePathStaysInsideDir(t *testing.T) {
	for _, p := range []string{"logs/../../etc/passwd", "../../x", "cache/../../../y"} {
		got := RelocatePath(p, "logs/", "/data/logs")
		if got != "/data/logs" && !strings.HasPrefix(got, "/data/logs/") || strings.Contains(got, "..") {
			t.Errorf("RelocatePath(%q) = %q, left the directory", p, got)
		}
	}
	if got := RelocatePath("logs/a/b.log", "logs/", "/d"); got != "/d/a/b.log" {
		t.Errorf("RelocatePath kept shape = %q", got)
	}
}
