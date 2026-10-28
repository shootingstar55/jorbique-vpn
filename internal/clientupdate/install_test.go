package clientupdate

import (
	"archive/zip"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsArchiveRejectsTraversalAndExtraFiles(t *testing.T) {
	for _, names := range [][]string{{"../escape.exe"}, {"other.exe"}, {"Jorbique-VPN-Extended.exe", "extra.dll"}, {"Jorbique-VPN-Extended.exe"}} {
		t.Run(names[0], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "update.zip")
			f, _ := os.Create(path)
			w := zip.NewWriter(f)
			for _, name := range names {
				e, err := w.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = e.Write([]byte("new executable"))
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			_ = f.Close()
			target, err := extractWindows(path)
			valid := len(names) == 1 && names[0] == "Jorbique-VPN-Extended.exe"
			if (err == nil) != valid {
				t.Fatalf("%v: %v", names, err)
			}
			if valid {
				data, _ := os.ReadFile(target)
				if string(data) != "new executable" {
					t.Fatal("wrong executable")
				}
			}
		})
	}
}

func TestReplacementAndRollback(t *testing.T) {
	for _, kind := range []string{"success", "launch failure", "changed target", "symlink", "rollback failure"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "client.exe")
			source := filepath.Join(dir, "new.exe")
			_ = os.WriteFile(target, []byte("old"), 0700)
			_ = os.WriteFile(source, []byte("new"), 0700)
			hash := sha256.Sum256([]byte("old"))
			if kind == "changed target" {
				_ = os.WriteFile(target, []byte("changed"), 0700)
			}
			if kind == "symlink" {
				_ = os.Remove(target)
				if err := os.Symlink(source, target); err != nil {
					t.Skip(err)
				}
			}
			called := false
			err := replaceClient(target, source, hash[:], func(path string) error {
				called = true
				if path != target {
					t.Fatal("wrong restart path")
				}
				if kind == "rollback failure" {
					_ = os.Remove(target)
					_ = os.Mkdir(target, 0700)
				}
				if kind == "launch failure" || kind == "rollback failure" {
					return errors.New("cannot launch")
				}
				return nil
			})
			data, _ := os.ReadFile(target)
			switch kind {
			case "success":
				if err != nil || !called || string(data) != "new" {
					t.Fatalf("%s %v", data, err)
				}
			case "launch failure":
				if err == nil || string(data) != "old" {
					t.Fatalf("original not restored: %s %v", data, err)
				}
			case "changed target", "symlink":
				if err == nil || called {
					t.Fatal("unsafe target accepted")
				}
			case "rollback failure":
				backups, _ := filepath.Glob(filepath.Join(dir, ".jorbique-previous-*"))
				if err == nil || len(backups) != 1 {
					t.Fatalf("backup lost: %v %v", backups, err)
				}
				data, _ := os.ReadFile(backups[0])
				if string(data) != "old" {
					t.Fatal("backup corrupted")
				}
			}
		})
	}
}
