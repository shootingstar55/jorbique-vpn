//go:build !windows

package winicon

import (
	"os"
	"testing"
)

func TestApplyTitleBarFromExeDirIsNoOp(t *testing.T) {
	dir := t.TempDir()
	ApplyTitleBarFromExeDir(dir)
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("dir touched: %v %v", entries, err)
	}
}
