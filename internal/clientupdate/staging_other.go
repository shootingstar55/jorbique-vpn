//go:build !windows

package clientupdate

import (
	"os"
	"path/filepath"
)

func newStageDir() (string, error) {
	root := filepath.Join(os.TempDir(), "jorbique-client-updates")
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	return os.MkdirTemp(root, "release-")
}
