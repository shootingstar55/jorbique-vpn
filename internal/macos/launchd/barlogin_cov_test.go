package launchd

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// otherAccount is a non-root account every system the tests run on resolves:
// "daemon" is in macOS's directory services and in Debian's /etc/passwd.
func otherAccount(t *testing.T) (name string, uid int, home string) {
	t.Helper()
	u, err := user.Lookup("daemon")
	if err != nil {
		t.Skipf("no daemon account here: %v", err)
	}
	uid, err = strconv.Atoi(u.Uid)
	if err != nil || uid == 0 {
		t.Skipf("daemon account has uid %q", u.Uid)
	}
	return u.Username, uid, u.HomeDir
}

// Run under sudo, the bar's agent belongs to whoever ran sudo — by uid first,
// by name when the uid does not resolve, and never to root itself.
func TestSudoAutostartUser(t *testing.T) {
	name, uid, home := otherAccount(t)
	cases := []struct {
		what, sudoUID, sudoUser string
		ok                      bool
	}{
		{"the uid sudo recorded", strconv.Itoa(uid), "", true},
		{"the name when the uid is unknown", "999999999", name, true},
		{"the name alone", "", name, true},
		{"a uid that is not a number", "not-a-uid", "", false},
		{"root by uid", "0", "", false},
		{"root by name", "", "root", false},
		{"a name nobody has", "", "no-such-user-for-launchd-test", false},
		{"nothing at all", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			t.Setenv("SUDO_UID", c.sudoUID)
			t.Setenv("SUDO_USER", c.sudoUser)
			gotUID, gotHome, ok := sudoAutostartUser()
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v", ok, c.ok)
			}
			if ok && (gotUID != uid || gotHome != home) {
				t.Errorf("got uid %d home %q, want %d %q", gotUID, gotHome, uid, home)
			}
		})
	}
}

// Root looking for the bar's owner walks /Users: hidden entries and Shared are
// not accounts, a name with no account is skipped, and the first account whose
// agent plist exists wins.
func TestFindAppAutostartUser(t *testing.T) {
	name, uid, _ := otherAccount(t)
	users := t.TempDir()
	for _, n := range []string{".hidden", "Shared", "a-no-such-user-for-launchd-test", name} {
		if err := os.Mkdir(filepath.Join(users, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(users)
	if err != nil {
		t.Fatal(err)
	}

	// The label climbs out of /Users/<name>/Library/LaunchAgents, so the plist
	// it names is one this test wrote, whichever account is being looked at.
	plist := filepath.Join(t.TempDir(), "agent.plist")
	if err := os.WriteFile(plist, []byte("<plist/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	label := strings.Repeat("../", 4) + strings.TrimSuffix(plist, ".plist")
	if got := barAgentPlistPath(filepath.Join("/Users", name), label); got != plist {
		t.Fatalf("label resolves to %q, want %q", got, plist)
	}
	gotUID, gotHome, ok := findAppAutostartUser(entries, label, false)
	if !ok || gotUID != uid || gotHome != filepath.Join("/Users", name) {
		t.Errorf("got %d %q %v, want %d /Users/%s", gotUID, gotHome, ok, uid, name)
	}

	// No account has the agent installed.
	if _, _, ok := findAppAutostartUser(entries, "com.example.launchd-test-absent", false); ok {
		t.Error("found an owner for an agent no account has")
	}
	// Nor is any of these an account launchd could have it loaded for. The
	// resolvable account is left out: asking about it would ask launchctl.
	if _, _, ok := findAppAutostartUser(entries[:3], DefaultBarLabel, true); ok {
		t.Error("found an owner among entries that are not accounts")
	}
}

func TestResolveAppAutostartUser(t *testing.T) {
	if os.Geteuid() != 0 {
		u, err := user.Current()
		if err != nil || u.HomeDir == "" {
			t.Skipf("no current user: %v", err)
		}
		uid, home, ok := resolveAppAutostartUser("")
		if !ok || uid != os.Getuid() || home != u.HomeDir {
			t.Errorf("got %d %q %v, want the current user %d %q", uid, home, ok, os.Getuid(), u.HomeDir)
		}
		return
	}
	if runtime.GOOS == "darwin" {
		t.Skip("as root on macOS this asks launchctl about every account")
	}
	_, wantUID, wantHome := otherAccount(t)
	t.Setenv("SUDO_UID", strconv.Itoa(wantUID))
	t.Setenv("SUDO_USER", "")
	if uid, home, ok := resolveAppAutostartUser(""); !ok || uid != wantUID || home != wantHome {
		t.Errorf("got %d %q %v, want the sudo user %d %q", uid, home, ok, wantUID, wantHome)
	}
	if _, err := os.Stat("/Users"); err == nil {
		t.Skip("this machine has a /Users to search")
	}
	t.Setenv("SUDO_UID", "")
	if _, _, ok := resolveAppAutostartUser(""); ok {
		t.Error("found an owner with no sudo user and no home directories to search")
	}
}
