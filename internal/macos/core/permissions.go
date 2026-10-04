package core

import (
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
)

// The installed data used to be root:admin and group-writable, so that the menu
// bar — running as the logged-in user — could write its log and its config
// without asking for a password. What it also meant is that any admin user on
// the machine, and anything running as them, could rewrite the sing-box config
// root later loads. A modified config can change routes and expose credentials, so
// that is a way to become root by editing a file.
//
// Nothing needs the group write any more: the bar's own log went to the user's
// ~/Library/Logs, and every write into the system-wide directories happens in a
// subcommand that escalates as a whole. The installer sets this; this repairs a
// machine that upgraded into it, and re-repairs anything that drifts.

// dataOwnerRepair describes the ownership one directory tree should have.
type dataOwnerRepair struct {
	path     string
	group    string // "wheel" or "admin"
	dirMode  fs.FileMode
	fileMode fs.FileMode
}

// RepairDataPermissions puts the installed data back to root ownership with no
// group write. It returns the paths it had to change, and does nothing at all
// unless it is root looking at an installed layout.
func RepairDataPermissions(p *Paths) []string {
	if p == nil || os.Geteuid() != 0 || !isInstalledBinDir(p.BinDir) {
		return nil
	}
	var changed []string
	for _, r := range repairOrder(p) {
		changed = append(changed, applyRepair(r)...)
	}
	return changed
}

// repairOrder lists the trees to repair, containing directories first: the
// config directory lives inside the data directory and has the stricter rule,
// and repairing the data directory afterwards would widen it right back.
func repairOrder(p *Paths) []dataOwnerRepair {
	return []dataOwnerRepair{
		{path: p.CacheDir, group: "wheel", dirMode: 0o755, fileMode: 0o644},
		{path: p.LogDir, group: "wheel", dirMode: 0o755, fileMode: 0o644},
		{path: p.DataDir, group: "wheel", dirMode: 0o755, fileMode: 0o644},
		// The configuration and the downloaded profiles: the admin group reads
		// them so the menu bar can show what is configured; root alone writes.
		{path: p.CfgDir, group: "admin", dirMode: 0o750, fileMode: 0o640},
	}
}

func applyRepair(r dataOwnerRepair) []string {
	if r.path == "" {
		return nil
	}
	gid, ok := groupID(r.group)
	if !ok {
		return nil
	}
	var changed []string
	_ = filepath.WalkDir(r.path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if repairEntry(r, gid, path, d) {
			changed = append(changed, path)
		}
		return nil
	})
	return changed
}

// repairEntry gives one walked entry the mode and ownership r asks for and
// reports whether it changed anything.
func repairEntry(r dataOwnerRepair, gid int, path string, d fs.DirEntry) bool {
	info, err := d.Info()
	if err != nil {
		return false
	}
	// Symlinks are followed by nothing here: chmod would land on the
	// target, which may be outside the tree entirely.
	if info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	want := r.fileMode
	if d.IsDir() {
		want = r.dirMode
	}
	touched := false
	if info.Mode().Perm() != want {
		if os.Chmod(path, want) == nil {
			touched = true
		}
	}
	if uid, g, ok := ownerOf(info); ok && (uid != 0 || g != gid) {
		if os.Chown(path, 0, gid) == nil {
			touched = true
		}
	}
	return touched
}

// groupID resolves a group name to its id. Missing groups are not an error to
// report: a machine without an "admin" group is not one to start chowning on a
// guess.
func groupID(name string) (int, bool) {
	g, err := user.LookupGroup(name)
	if err != nil {
		return 0, false
	}
	id, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, false
	}
	return id, true
}

// ownerOf reads uid/gid off a FileInfo. Portable-ish: the syscall stat is
// darwin's, which is the only platform this file builds for.
func ownerOf(info fs.FileInfo) (uid int, gid int, ok bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return 0, 0, false
	}
	return int(st.Uid), int(st.Gid), true
}
