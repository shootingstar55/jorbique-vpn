package core

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// The repair walks a tree and narrows what is wider than the rule. It is run
// here as whoever runs the tests, against the group the tree already has, so
// the only change it can make is the mode — and a symlink, which chmod would
// follow out of the tree, is not touched at all.
func TestRepairNarrowsModesAndLeavesSymlinksAlone(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "profiles")
	mustMkdir(t, sub)
	file := filepath.Join(sub, "singbox.json")
	mustWrite(t, file, "client\n")
	target := filepath.Join(t.TempDir(), "elsewhere")
	mustWrite(t, target, "not ours\n")
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	for path, mode := range map[string]os.FileMode{root: 0o750, sub: 0o775, file: 0o664, target: 0o666} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}

	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	_, gid, ok := ownerOf(info)
	if !ok {
		t.Skip("no owner information on this platform")
	}
	g, err := user.LookupGroupId(strconv.Itoa(gid))
	if err != nil {
		t.Skipf("group %d has no name here: %v", gid, err)
	}

	changed := applyRepair(dataOwnerRepair{path: root, group: g.Name, dirMode: 0o750, fileMode: 0o640})
	if want := []string{sub, file}; !reflect.DeepEqual(changed, want) {
		t.Errorf("changed %v, want %v", changed, want)
	}
	for path, want := range map[string]os.FileMode{root: 0o750, sub: 0o750, file: 0o640, target: 0o666} {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != want {
			t.Errorf("%s mode %v, want %v", path, st.Mode().Perm(), want)
		}
	}
}

func TestRepairSkipsWhatItCannotPlace(t *testing.T) {
	if got := applyRepair(dataOwnerRepair{group: "wheel"}); got != nil {
		t.Errorf("an unset path was repaired: %v", got)
	}
	if got := applyRepair(dataOwnerRepair{path: t.TempDir(), group: "no-such-group-for-repair"}); got != nil {
		t.Errorf("a tree was repaired for a group that does not exist: %v", got)
	}

	// An entry gone between the directory read and the repair is not touched.
	dir := t.TempDir()
	gone := filepath.Join(dir, "gone")
	mustWrite(t, gone, "x")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	if repairEntry(dataOwnerRepair{fileMode: 0o640}, os.Getgid(), gone, entries[0]) {
		t.Error("reported a change to a file that is no longer there")
	}
}

func TestPathOverridesReplaceEveryDefaultTheyName(t *testing.T) {
	defaults := EffectivePaths{CfgDir: "cfg", LogDir: "logs", StartedListFilename: "s.list", SingboxFallbackFile: "sb.json", RunDir: "/run/a", RunDirUser: ".a"}
	over := EffectivePaths{CfgDir: "c2", LogDir: "l2", StartedListFilename: "s2.list", SingboxFallbackFile: "sb2.json", RunDir: "/run/b", RunDirUser: ".b"}
	if got := pathOverrides(over, defaults); got != over {
		t.Errorf("pathOverrides = %+v, want %+v", got, over)
	}
	if got := pathOverrides(EffectivePaths{}, defaults); got != defaults {
		t.Errorf("empty overrides changed the defaults: %+v", got)
	}
}

// A build outside the install location lays out next to its own binary, with
// absolute overrides taken as they are. Everything it creates is under the
// overrides and the temporary home.
func TestNewPathsWithEffectiveHonoursOverrides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	tmp := t.TempDir()
	ep := &EffectivePaths{
		CfgDir:     filepath.Join(tmp, "cfg"),
		LogDir:     filepath.Join(tmp, "logs"),
		RunDir:     filepath.Join(tmp, "run"),
		RunDirUser: ".mvpn-test",
	}
	p := NewPathsWithEffective(ep)

	if want := filepath.Dir(resolveExecutable()); p.BinDir != want {
		t.Errorf("BinDir = %q, want %q", p.BinDir, want)
	}
	if p.CfgDir != ep.CfgDir || p.CfgFile != filepath.Join(ep.CfgDir, "vpn_cfg.json") || p.LogDir != ep.LogDir {
		t.Errorf("overrides not honoured: %+v", p)
	}
	wantRun := filepath.Join(home, ".mvpn-test", "run")
	if os.Geteuid() == 0 {
		wantRun = ep.RunDir
	}
	if p.RunDir != wantRun {
		t.Errorf("RunDir = %q, want %q", p.RunDir, wantRun)
	}
	for _, dir := range []string{p.RunDir, p.LogDir} {
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			t.Errorf("%s was not created: %v", dir, err)
		}
	}
}

func TestStripTrailingCommas(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"a":[1,2,],}`, `{"a":[1,2]}`},
		{"[1,\n\t ]", "[1\n\t ]"},
		{`{"s":"a,]","t":"q\",}"}`, `{"s":"a,]","t":"q\",}"}`},
		{`{"p":"c:\\",}`, `{"p":"c:\\"}`},
		{`[1,2]`, `[1,2]`},
	}
	for _, c := range cases {
		if got := string(StripTrailingCommas([]byte(c.in))); got != c.want {
			t.Errorf("StripTrailingCommas(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestToolNotFoundSuggestsInstallingTheCore(t *testing.T) {
	var e *CLIError
	if !errors.As(ToolNotFound("sing-box"), &e) {
		t.Fatal("ToolNotFound is not a CLIError")
	}
	if !strings.Contains(e.Suggestion(), "sing-box core") {
		t.Errorf("suggestion for a missing tool = %q", e.Suggestion())
	}
}
