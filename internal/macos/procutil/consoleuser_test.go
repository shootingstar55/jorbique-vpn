package procutil

import (
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"testing"
)

// ConsoleUserOf answers "who is logged in at the screen", which decides who the
// daemon runs things as and whose Keychain gets the credentials. It used to
// shell out to stat and parse the output; these pin the replacement against a
// file whose owner the test knows.

func TestConsoleUserOfReadsTheOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "console")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	uid, name, ok := ConsoleUserOf(path)
	if !ok {
		t.Fatalf("ConsoleUserOf(%s) reported failure for a file we just created", path)
	}
	if uid != os.Getuid() {
		t.Errorf("uid = %d, want %d", uid, os.Getuid())
	}
	if u, err := user.LookupId(strconv.Itoa(os.Getuid())); err == nil && name != u.Username {
		t.Errorf("username = %q, want %q", name, u.Username)
	}
}

// No console device means nobody is logged in — a headless boot, or the window
// server not up yet. That has to read as "no user", not as uid 0, or the daemon
// would run a login user's work as root.
func TestConsoleUserOfMissingPath(t *testing.T) {
	uid, name, ok := ConsoleUserOf(filepath.Join(t.TempDir(), "does-not-exist"))
	if ok {
		t.Error("a missing console device must not report a user")
	}
	if uid != 0 || name != "" {
		t.Errorf("got uid=%d name=%q, want the zero answer", uid, name)
	}
}
