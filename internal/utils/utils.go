package utils

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// GetProjectRoot attempts to find project root (directory containing singbox_cfg.yml)
func GetProjectRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		wd = ""
	}

	// Walk up a few levels until we find singbox_cfg.yml (repo root in dev).
	for i := 0; i < 8 && wd != ""; i++ {
		if _, err := os.Stat(filepath.Join(wd, "singbox_cfg.yml")); err == nil {
			return wd
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			break
		}
		wd = parent
	}

	// Fallback: exe directory (when launched from Explorer, cwd may be wrong)
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		if _, err := os.Stat(filepath.Join(exeDir, "singbox_cfg.yml")); err == nil {
			return exeDir
		}
	}
	if wd != "" {
		return wd
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	return ""
}

// RelocatePath rewrites a path that came out of a sing-box profile into a
// directory on this machine.
//
// Both clients take the profile whole now, so a path inside it is a path on
// whatever machine wrote it: relative ones resolve against sing-box's working
// directory, which differs between a service start and a manual one, and
// absolute ones are somebody else's disk. A path under prefix keeps its shape
// below that prefix — "cache/tun_system/cache.db" stays one cache per profile —
// and anything else keeps only its file name.
func RelocatePath(p, prefix, dir string) string {
	p = strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	if p == "" {
		return dir
	}
	rest := path.Base(p)
	if strings.HasPrefix(p, prefix) {
		rest = strings.TrimPrefix(p, prefix)
	}
	rest = strings.Trim(rest, "/")
	if rest == "" {
		return dir
	}
	return dir + "/" + rest
}
