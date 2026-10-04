// Package fsatomic writes a file in one step.
//
// The client replaces files that something else reads the moment they appear:
// LaunchDaemon plists, which launchd loads, and the configs sing-box reads. A
// truncate-and-fill leaves a window where the reader sees half a file, so the
// write goes to a temporary beside the target, which is renamed over it — and
// removed when the rename fails, so a directory something is watching never
// fills with half-written files.
package fsatomic

import (
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// WriteFile creates path's parent directories if needed and replaces path with
// data in a single rename. The temporary lives beside the target so the rename
// cannot cross a filesystem, and is removed on every failure after it exists.
//
// mode is applied explicitly rather than left to the umask: a plist launchd
// cannot read, or a resolver file mDNSResponder cannot read, is the same as no
// file at all, and the process doing the writing is usually the daemon, whose
// umask is not the user's.
func WriteFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp." + uuid.NewString()
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
