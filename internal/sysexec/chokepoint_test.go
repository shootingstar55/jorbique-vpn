package sysexec

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// directExec matches a call to os/exec's constructors, and not to this
// package's wrappers of the same name.
var directExec = regexp.MustCompile(`(^|[^A-Za-z0-9_.])exec\.Command`)

// TestSysexecIsTheOnlyChokepoint keeps the claim in docs/external-commands.md
// honest: outside this package, nothing in the client starts a program on its
// own terms — on either platform. A new exec.Command anywhere else fails here,
// and the way past it is to call sysexec, not to add an exception.
func TestSysexecIsTheOnlyChokepoint(t *testing.T) {
	root := filepath.Join("..", "..")

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Dot-directories are not ours to police, and in CI one of them is
			// the module cache: GOMODCACHE is set to .go/pkg/mod inside the
			// checkout so it can be cached between jobs, which put every
			// dependency's exec.Command in front of this test.
			if path != root && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			switch d.Name() {
			case "node_modules", "vendor", "frontend", "release", "logs":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if rel == "internal/sysexec/sysexec.go" {
			return nil
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if !directExec.Match(src) {
			return nil
		}
		t.Errorf("%s calls exec.Command directly; use sysexec.Command so the "+
			"no-shell and absolute-path rules apply", rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}
