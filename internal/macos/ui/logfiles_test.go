package ui

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"jorbiquevpn/internal/macos/core"
)

func logTestPaths(t *testing.T) *core.Paths {
	t.Helper()
	root := t.TempDir()
	p := &core.Paths{SystemLogDir: filepath.Join(root, "system"), LogDir: filepath.Join(root, "user")}
	for dir, names := range map[string][]string{
		p.SystemLogDir: {"app.log", "Jorbique VPN.log", "notes.txt"},
		p.LogDir:       {"app.log", "bar.err.log"},
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, n := range names {
			if err := os.WriteFile(filepath.Join(dir, n), []byte("\x1b[31m"+n+"\x1b[0m\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return p
}

func TestLogFileNamesListsRootsThenTheBarsOwn(t *testing.T) {
	p := logTestPaths(t)
	got := logFileNames(p)
	want := []string{"Jorbique VPN.log", "app.log", "menu bar/app.log", "menu bar/bar.err.log"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("logFileNames = %v, want %v", got, want)
	}
}

func TestLogPathTakesOnlyListedNames(t *testing.T) {
	p := logTestPaths(t)
	if path, err := logPath(p, "menu bar/app.log"); err != nil || path != filepath.Join(p.LogDir, "app.log") {
		t.Errorf("bar log resolved to %q, %v", path, err)
	}
	if path, err := logPath(p, "app.log"); err != nil || path != filepath.Join(p.SystemLogDir, "app.log") {
		t.Errorf("root log resolved to %q, %v", path, err)
	}
	for _, bad := range []string{"../app.log", "menu bar/../x.log", "/etc/passwd", ".hidden.log", "notes.txt", "sub/app.log"} {
		if _, err := logPath(p, bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestReadLogStripsColours(t *testing.T) {
	p := logTestPaths(t)
	got, err := readLog(p, "Jorbique VPN.log")
	if err != nil || got != "Jorbique VPN.log\n" {
		t.Errorf("readLog = %q, %v", got, err)
	}
}
