package core

import (
	"os"
	"path/filepath"
	"testing"
)

// A build run from anywhere but the install location keeps its data next to the
// binary. That is what makes an extracted archive or a developer tree work
// without touching /Library, and it is the behaviour every test here relies on.
func TestPathsStayNextToTheBinaryWhenNotInstalled(t *testing.T) {
	dir := t.TempDir()
	if isInstalledBinDir(dir) {
		t.Fatalf("%s should not count as an install location", dir)
	}
	if got := joinBase(dir, "cfg"); got != filepath.Join(dir, "cfg") {
		t.Errorf("cfg = %q", got)
	}
}

// The installed binary uses the platform's locations, not a pile under
// /usr/local/sbin.
func TestInstalledLayoutUsesPlatformDirectories(t *testing.T) {
	if !isInstalledBinDir(InstalledBinDir) {
		t.Error("the install directory is not recognised as one")
	}
	for _, d := range []string{InstalledSupport, InstalledLogs, InstalledCaches, InstalledRunDir} {
		if !filepath.IsAbs(d) {
			t.Errorf("%q is not absolute", d)
		}
	}
}

// An absolute override is the final path; a bare name is a directory under the
// base. Without this an override could only ever nest inside the layout.
func TestJoinBaseHonoursAbsoluteOverrides(t *testing.T) {
	if got := joinBase("/base", "/somewhere/else"); got != "/somewhere/else" {
		t.Errorf("absolute override = %q", got)
	}
	if got := joinBase("/base", "name"); got != "/base/name" {
		t.Errorf("relative name = %q", got)
	}
}

// The VPN entries name their profiles as "cfg/singbox.json" — a relative path
// with a slash in it. That used to resolve against the binaries' directory
// because everything lived in one place; after the move it has to land in the
// data directory, or the client writes profiles into the read-only install and
// reports every one of them as "config unreadable".
func TestConfigRelativePathsResolveIntoTheDataDirectory(t *testing.T) {
	p := &Paths{
		BinDir:  InstalledBinDir,
		DataDir: InstalledSupport,
		CfgDir:  filepath.Join(InstalledSupport, "cfg"),
	}
	want := filepath.Join(InstalledSupport, "cfg", "singbox.json")
	if got := p.ResolveFromCfg("cfg/singbox.json"); got != want {
		t.Errorf("ResolveFromCfg = %q, want %q", got, want)
	}
	if got := p.AbsolutePathForConfigField("cfg/singbox.json"); got != filepath.Join(InstalledSupport, "cfg", "singbox.json") {
		t.Errorf("AbsolutePathForConfigField = %q", got)
	}
	// A bare name still means "in the config directory".
	if got := p.ResolveFromCfg("vpn_cfg.json"); got != filepath.Join(InstalledSupport, "cfg", "vpn_cfg.json") {
		t.Errorf("bare name = %q", got)
	}
	// Binaries keep resolving next to the binary: sing-box is installed there,
	// not in the data directory.
	if got := p.ResolveFromBin("sing-box"); got != filepath.Join(InstalledBinDir, "sing-box") {
		t.Errorf("ResolveFromBin = %q", got)
	}
}

// Without an install — a developer tree — everything still resolves next to the
// binary, which is what keeps a copied directory working.
func TestConfigRelativePathsFallBackToTheBinary(t *testing.T) {
	p := &Paths{BinDir: "/opt/tree", CfgDir: "/opt/tree/cfg"}
	if got := p.ResolveFromCfg("cfg/singbox.json"); got != "/opt/tree/cfg/singbox.json" {
		t.Errorf("ResolveFromCfg = %q", got)
	}
}

// /Library/Caches/JorbiqueVPN already names what it holds; a "cache" inside it
// only repeats the name, which is how the sing-box cache ended up at
// Caches/JorbiqueVPN/cache/jorbique_vpn/cache.db.
func TestInstalledCacheDirDoesNotRepeatItsName(t *testing.T) {
	if got := joinBase(InstalledCaches, ""); got != InstalledCaches {
		t.Errorf("cache dir = %q, want %q", got, InstalledCaches)
	}
	if got := joinBase(InstalledLogs, ""); got != InstalledLogs {
		t.Errorf("log dir = %q, want %q", got, InstalledLogs)
	}
}

// The autostart list and the last exit IP are state, not logs. They lived in
// the log directory because that used to be the only place the client wrote to.
func TestStateFilesSitWithTheConfigNotTheLogs(t *testing.T) {
	p := &Paths{
		BinDir:              InstalledBinDir,
		DataDir:             InstalledSupport,
		LogDir:              InstalledLogs,
		StartedListFilename: "started.list",
	}
	if got := p.StartedListURL(); got != filepath.Join(InstalledSupport, "started.list") {
		t.Errorf("started list = %q", got)
	}
	if got := p.StateDir(); got != InstalledSupport {
		t.Errorf("state dir = %q", got)
	}
}

// The autostart list used to carry a built-in path of "logs/started.list".
// After the layout move that resolved to a logs directory inside Application
// Support which nothing creates, so every save failed with ENOENT and the client
// forgot which tunnels to bring back — the one thing the list exists for.
func TestTheAutostartListHasNoBuiltInPathToGetWrong(t *testing.T) {
	if p := BuiltinConfig().StartedListPath; p != nil {
		t.Fatalf("built-in started_list_path is back: %q", *p)
	}

	dir := t.TempDir()
	paths := &Paths{
		BinDir:              filepath.Join(dir, "libexec"),
		DataDir:             filepath.Join(dir, "support"),
		CfgDir:              filepath.Join(dir, "support", "cfg"),
		CfgFile:             filepath.Join(dir, "support", "cfg", "vpn_cfg.json"),
		LogDir:              filepath.Join(dir, "logs"),
		StartedListFilename: "started.list",
	}
	cfg, err := ReadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StartedListPath != nil {
		t.Errorf("ReadConfig invented a path: %q", *cfg.StartedListPath)
	}
	if got, want := paths.StartedListURL(), filepath.Join(paths.DataDir, "started.list"); got != want {
		t.Errorf("StartedListURL = %q, want %q", got, want)
	}
}

// An install under a prefix the packager resolved — Homebrew picks its own —
// is still an install, and its data belongs in /Library rather than next to the
// binary. /usr/local is our default, not the only prefix there is.
func TestAnInstallOutsideUsrLocalIsStillAnInstall(t *testing.T) {
	for _, dir := range []string{
		"/opt/homebrew/libexec/jorbique-vpn",
		"/opt/local/libexec/jorbique-vpn",
	} {
		if !isInstalledBinDir(dir) {
			t.Errorf("%s is not recognised as an install", dir)
		}
	}
	for _, dir := range []string{
		"/Users/someone/build/jorbique-vpn",
		"/usr/local/libexec/jorbique-vpn-other",
		"/opt/homebrew/libexec",
	} {
		if isInstalledBinDir(dir) {
			t.Errorf("%s should not count as an install", dir)
		}
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The menu bar runs as the logged-in user and keeps its log in that user's own
// log directory. /Library/Logs is root's: it used to be handed to whoever
// logged in first so the bar could append to it, which is how a system-wide
// directory ends up owned by one account.
//
// What root wrote stays where it is: the tunnel logs, and the app.log of a
// renew-certs the daemon ran. SystemLogDir is how the user's side finds them,
// and it has to be taken before LogDir moves into the home directory.
func TestTheUserFacingLogDirectoryIsPerUser(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	p := newPathsAt(nil, InstalledBinDir, 501)
	if want := filepath.Join(home, "Library/Logs/JorbiqueVPN"); p.LogDir != want {
		t.Errorf("LogDir = %q, want %q", p.LogDir, want)
	}
	if p.SystemLogDir != InstalledLogs {
		t.Errorf("SystemLogDir = %q, want %q", p.SystemLogDir, InstalledLogs)
	}
	if want := filepath.Join(InstalledLogs, "intra.log"); p.LogFile("intra") != want {
		t.Errorf("tunnel log = %q, want %q", p.LogFile("intra"), want)
	}
}

// Root writes its app.log into LogDir and everyone reads it from SystemLogDir,
// so for root they are one directory. So they are for a build tree, where the
// logs stay next to the binary whoever runs it.
func TestSystemLogDirIsLogDirWhenNothingSplitsThem(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	logs := filepath.Join(dir, "logs")
	for name, p := range map[string]*Paths{
		"installed, as root": newPathsAt(&EffectivePaths{LogDir: logs, RunDir: filepath.Join(dir, "run")}, InstalledBinDir, 0),
		"build tree":         newPathsAt(nil, dir, 501),
	} {
		if p.LogDir != logs || p.SystemLogDir != logs {
			t.Errorf("%s: LogDir = %q, SystemLogDir = %q, want both %q", name, p.LogDir, p.SystemLogDir, logs)
		}
	}
}

func TestLogFileReadsSystemLogsWithSeparateUserAppLogs(t *testing.T) {
	p := &Paths{LogDir: "/Users/test/Library/Logs/JorbiqueVPN", SystemLogDir: InstalledLogs}
	if got, want := p.LogFile("intra"), filepath.Join(InstalledLogs, "intra.log"); got != want {
		t.Fatalf("tunnel log = %q, want %q", got, want)
	}
	p.SystemLogDir = ""
	if got, want := p.LogFile("intra"), filepath.Join(p.LogDir, "intra.log"); got != want {
		t.Fatalf("legacy paths log = %q, want %q", got, want)
	}
}

// The daemon's control socket lives in the daemon's run directory, and an
// unprivileged process has to find the same one: its own Paths point at the
// user's run directory, which is the point of having two. Getting this wrong is
// silent — the client reports "the daemon is not running" while it is.
func TestTheDaemonRunDirIsTheOneTheDaemonUses(t *testing.T) {
	if got := (&AppConfig{}).EffectivePaths().RunDir; got != InstalledRunDir {
		t.Errorf("effective run dir = %q, want %q — the daemon and its callers would look in different places", got, InstalledRunDir)
	}
	// A configuration that names its own wins, for both sides.
	cfg := &AppConfig{Paths: &PathsSection{RunDir: "/var/run/somewhere-else"}}
	if got := cfg.EffectivePaths().RunDir; got != "/var/run/somewhere-else" {
		t.Errorf("an explicit run_dir was ignored: %q", got)
	}
}

// run_dir comes from the config the daemon and the menu bar both read, and the
// two disagreeing about it means the bar cannot find the socket at all. The
// built-in default has to survive the trip through the config, not only through
// the constant.
func TestRunDirDefaultsThroughTheConfig(t *testing.T) {
	var cfg AppConfig
	if got := cfg.EffectivePaths().RunDir; got != InstalledRunDir {
		t.Errorf("run dir with no config = %q, want %q", got, InstalledRunDir)
	}
	cfg.Paths = &PathsSection{RunDir: "/var/run/somewhere-else"}
	if got := cfg.EffectivePaths().RunDir; got != "/var/run/somewhere-else" {
		t.Errorf("a configured run dir was discarded: %q", got)
	}
}
