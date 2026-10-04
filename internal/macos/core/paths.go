package core

import (
	"os"
	"path/filepath"
	"strings"
)

// Installed layout. The client used to keep everything — binaries, configs,
// downloaded profiles with their private keys, logs and the DNS cache — in one
// root-owned directory under /usr/local/sbin, which is neither where macOS
// looks for any of it nor a place a user can manage. Installed builds now use
// the locations the platform defines; a build run from anywhere else (a
// developer tree, an extracted archive) keeps everything next to the binary,
// which is what makes it portable.
const (
	InstalledBinDir  = "/usr/local/libexec/jorbique-vpn"
	InstalledSupport = "/Library/Application Support/JorbiqueVPN"
	InstalledLogs    = "/Library/Logs/JorbiqueVPN"
	InstalledCaches  = "/Library/Caches/JorbiqueVPN"
	InstalledRunDir  = "/var/run/jorbique-vpn"
)

type Paths struct {
	BinDir string
	// DataDir is what a relative path in the config resolves against — the VPN
	// entries name their profiles as "cfg/singbox.json". It used to be the same
	// directory as the binaries, because everything lived in one place; now the
	// binaries are in libexec and the data is not, and joining "cfg/…" onto
	// BinDir sent every profile read and write into the read-only install
	// directory.
	DataDir             string
	CfgDir              string
	CfgFile             string
	RunDir              string
	LogDir              string
	SystemLogDir        string // daemon and tunnel logs, also read by the user CLI
	CacheDir            string
	StartedListFilename string
	SingboxFallbackFile string
}

// resolveExecutable returns the canonical absolute path to the running executable.
func resolveExecutable() string {
	exe, err := os.Executable()
	if err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			return real
		}
		return exe
	}
	arg0 := os.Args[0]
	if filepath.IsAbs(arg0) {
		if real, err := filepath.EvalSymlinks(arg0); err == nil {
			return real
		}
		return arg0
	}
	cwd, _ := os.Getwd()
	abs := filepath.Join(cwd, arg0)
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return abs
}

// NewPaths creates Paths with built-in defaults (used before config is read).
func NewPaths() *Paths {
	return NewPathsWithEffective(nil)
}

// NewPathsWithEffective creates Paths using the given effective overrides.
func NewPathsWithEffective(ep *EffectivePaths) *Paths {
	return newPathsAt(ep, filepath.Dir(resolveExecutable()), os.Geteuid())
}

// newPathsAt is NewPathsWithEffective for a given binary directory and user.
// Split out for testing: binDir comes from the running
// executable, and the installed layout — where the user's logs and root's part
// ways — is otherwise reachable only from a client that is actually installed.
func newPathsAt(ep *EffectivePaths, binDir string, euid int) *Paths {
	defaults := EffectivePaths{CfgDir: "cfg", LogDir: "logs", StartedListFilename: "started.list", SingboxFallbackFile: "singbox.json", RunDir: InstalledRunDir, RunDirUser: ".jorbique-vpn"}
	if ep != nil {
		defaults = pathOverrides(*ep, defaults)
	}
	cfgDirName, logDirName := defaults.CfgDir, defaults.LogDir
	cacheDirName := "cache"

	// An installed binary keeps its data where macOS expects it; anywhere else
	// the layout stays relative to the binary so the tree can be copied around.
	dataDir := binDir
	logBase, cacheBase := binDir, binDir
	if isInstalledBinDir(binDir) {
		dataDir, logBase, cacheBase = InstalledSupport, InstalledLogs, InstalledCaches
		// /Library/Logs/JorbiqueVPN and /Library/Caches/JorbiqueVPN already name
		// what they hold; a "logs" or "cache" inside them only repeats it.
		logDirName = resolveDirName(logDirName, "logs", "")
		cacheDirName = ""
	}

	cfgDir := joinBase(dataDir, cfgDirName)
	cfgFile := filepath.Join(cfgDir, "vpn_cfg.json")
	logDir := joinBase(logBase, logDirName)
	systemLogDir := logDir
	// The menu bar runs as the logged-in user and logs to that user's own log
	// directory. /Library/Logs belongs to root, which writes the daemon and
	// tunnel logs there; it used to be handed to whoever logged in first so the
	// bar could append to it, and a system-wide directory owned by a user is
	// how one account ends up able to rewrite another's logs.
	if isInstalledBinDir(binDir) && euid != 0 {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			logDir = filepath.Join(home, "Library/Logs/JorbiqueVPN")
		}
	}
	cacheDir := joinBase(cacheBase, cacheDirName)

	var runDir string
	if euid == 0 {
		runDir = defaults.RunDir
	} else {
		home, _ := os.UserHomeDir()
		if isInstalledBinDir(binDir) && (ep == nil || ep.RunDirUser == "") {
			runDir = filepath.Join(home, "Library/Application Support/JorbiqueVPN/run")
		} else {
			runDir = filepath.Join(home, defaults.RunDirUser, "run")
		}
	}

	_ = os.MkdirAll(runDir, 0o755)
	_ = os.MkdirAll(logDir, 0o755)

	return &Paths{
		BinDir:              binDir,
		DataDir:             dataDir,
		CfgDir:              cfgDir,
		CfgFile:             cfgFile,
		RunDir:              runDir,
		LogDir:              logDir,
		SystemLogDir:        systemLogDir,
		CacheDir:            cacheDir,
		StartedListFilename: defaults.StartedListFilename,
		SingboxFallbackFile: defaults.SingboxFallbackFile,
	}
}

func pathOverrides(overrides, defaults EffectivePaths) EffectivePaths {
	if overrides.CfgDir != "" {
		defaults.CfgDir = overrides.CfgDir
	}
	if overrides.LogDir != "" {
		defaults.LogDir = overrides.LogDir
	}
	if overrides.StartedListFilename != "" {
		defaults.StartedListFilename = overrides.StartedListFilename
	}
	if overrides.SingboxFallbackFile != "" {
		defaults.SingboxFallbackFile = overrides.SingboxFallbackFile
	}
	if overrides.RunDir != "" {
		defaults.RunDir = overrides.RunDir
	}
	if overrides.RunDirUser != "" {
		defaults.RunDirUser = overrides.RunDirUser
	}
	return defaults
}

// isInstalledBinDir reports whether the binary runs from the installed
// location.
func isInstalledBinDir(binDir string) bool {
	clean := filepath.Clean(binDir)
	if clean == InstalledBinDir {
		return true
	}
	// A prefix other than /usr/local is still an installed client: Homebrew
	// resolves its own (/opt/homebrew on Apple silicon), and a package built for
	// one is not a different program. What identifies the install is the shape
	// of the path, not the prefix someone chose to put it under.
	return strings.HasSuffix(clean, "/libexec/jorbique-vpn")
}

// joinBase treats an absolute override as the final path and a bare name as a
// directory under base.
func joinBase(base, name string) string {
	if filepath.IsAbs(name) {
		return filepath.Clean(name)
	}
	return filepath.Join(base, name)
}

// resolveDirName drops the default relative name once the base is a directory
// of its own: "/Library/Logs/JorbiqueVPN/logs" helps nobody.
func resolveDirName(name, defaultName, installedName string) string {
	if name == defaultName {
		return installedName
	}
	return name
}

// StartedListURL is where the autostart list lives. It is state, not a log.
func (p *Paths) StartedListURL() string {
	return filepath.Join(p.dataBase(), p.StartedListFilename)
}

// StateDir is where small pieces of state live: the autostart list, the last
// exit IP. Same directory as the configuration, because that is what they are —
// state the client keeps, not logs.
func (p *Paths) StateDir() string { return p.dataBase() }

func (p *Paths) PIDFile(name string) string { return filepath.Join(p.RunDir, name+".pid") }
func (p *Paths) LogFile(name string) string {
	dir := p.SystemLogDir
	if dir == "" {
		dir = p.LogDir
	}
	return filepath.Join(dir, name+".log")
}
func (p *Paths) ExecutablePath() string {
	return resolveExecutable()
}

func (p *Paths) ResolveFromBin(rel string) string {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return p.BinDir
	}
	if filepath.IsAbs(rel) {
		return filepath.Clean(rel)
	}
	return filepath.Clean(filepath.Join(p.BinDir, rel))
}

func (p *Paths) ResolveFromCfg(rel string) string {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return p.CfgDir
	}
	if filepath.IsAbs(rel) {
		return filepath.Clean(rel)
	}
	if strings.Contains(rel, "/") {
		return filepath.Clean(filepath.Join(p.dataBase(), rel))
	}
	return filepath.Clean(filepath.Join(p.CfgDir, rel))
}

// dataBase is what a relative path from the config resolves against, falling
// back to the binary's directory for a Paths built without one.
func (p *Paths) dataBase() string {
	if strings.TrimSpace(p.DataDir) != "" {
		return p.DataDir
	}
	return p.BinDir
}

func (p *Paths) AbsolutePathForConfigField(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return p.CfgDir
	}
	if filepath.IsAbs(s) {
		return filepath.Clean(s)
	}
	if strings.Contains(s, "/") {
		return filepath.Clean(filepath.Join(p.dataBase(), s))
	}
	return filepath.Clean(filepath.Join(p.CfgDir, s))
}

// DaemonRunDir is the run directory the root daemon uses — where its control
// socket lives. An unprivileged process cannot ask its own Paths for it: those
// point at the user's own run directory, which is the whole point of having
// two. It reads the same configuration the daemon reads, so an installation
// that overrides the path keeps both sides in agreement.
func DaemonRunDir() string {
	p := NewPaths()
	if os.Geteuid() == 0 {
		return p.RunDir
	}
	cfg, err := ReadConfig(p)
	if err != nil {
		// No config to read: a fresh install, or a menu bar that started before
		// the daemon wrote one. The daemon has nothing else to go on either, so
		// falling back to the built-in default keeps the two in agreement.
		return InstalledRunDir
	}
	// EffectivePaths answers this itself — it defaults run_dir to
	// InstalledRunDir — so there is no empty case left to second-guess here.
	return cfg.EffectivePaths().RunDir
}
