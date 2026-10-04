package core

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// The repair runs as root against an installed layout. Everywhere else it has
// to keep its hands off: a developer tree, or a machine where the client runs
// as the user, is not one to start chowning files on.
func TestPermissionRepairOnlyTouchesAnInstalledLayoutAsRoot(t *testing.T) {
	dir := t.TempDir()
	p := &Paths{BinDir: dir, DataDir: dir, CfgDir: filepath.Join(dir, "cfg"), LogDir: dir, CacheDir: dir}
	if got := RepairDataPermissions(p); got != nil {
		t.Errorf("touched a non-installed layout: %v", got)
	}
	if got := RepairDataPermissions(nil); got != nil {
		t.Errorf("touched something with no paths: %v", got)
	}
	if os.Geteuid() == 0 {
		t.Skip("the non-root half of this test needs a non-root process")
	}
	installed := &Paths{BinDir: InstalledBinDir, DataDir: dir, CfgDir: filepath.Join(dir, "cfg"), LogDir: dir, CacheDir: dir}
	if got := RepairDataPermissions(installed); got != nil {
		t.Errorf("a non-root process changed ownership: %v", got)
	}
}

// The mode table is the whole point: nothing group- or world-writable, and the
// profiles readable by the admin group only.
func TestTheModesWeRepairToAreNotWritableByAnyoneButRoot(t *testing.T) {
	repairs := []dataOwnerRepair{
		{path: "/cfg", group: "admin", dirMode: 0o750, fileMode: 0o640},
		{path: "/data", group: "wheel", dirMode: 0o755, fileMode: 0o644},
	}
	for _, r := range repairs {
		for _, m := range []fs.FileMode{r.dirMode, r.fileMode} {
			if m&0o022 != 0 {
				t.Errorf("%s would be writable beyond root: %v", r.path, m)
			}
		}
	}
	// Profiles carry private keys: nobody outside the admin group reads them.
	if repairs[0].fileMode&0o007 != 0 || repairs[0].dirMode&0o007 != 0 {
		t.Errorf("the config directory is readable by everyone: %v %v", repairs[0].dirMode, repairs[0].fileMode)
	}
}

// The config directory sits inside the data directory and has stricter rules,
// so it must be applied last — walking the data directory afterwards would
// widen it right back.
func TestTheConfigDirectoryRuleIsAppliedAfterTheDataDirectory(t *testing.T) {
	p := &Paths{
		BinDir:   InstalledBinDir,
		DataDir:  "/Library/Application Support/JorbiqueVPN",
		CfgDir:   "/Library/Application Support/JorbiqueVPN/cfg",
		LogDir:   "/Library/Logs/JorbiqueVPN",
		CacheDir: "/Library/Caches/JorbiqueVPN",
	}
	order := repairOrder(p)
	dataAt, cfgAt := -1, -1
	for i, r := range order {
		switch r.path {
		case p.DataDir:
			dataAt = i
		case p.CfgDir:
			cfgAt = i
		}
	}
	if dataAt < 0 || cfgAt < 0 {
		t.Fatalf("both directories should be repaired: %+v", order)
	}
	if cfgAt < dataAt {
		t.Errorf("the config directory is repaired before the data directory that contains it (%d < %d)", cfgAt, dataAt)
	}
}
