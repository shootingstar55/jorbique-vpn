package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// What the postinstall lays down is a privilege boundary, not a convenience.
// The daemon consumes root-owned configuration. Upgrades must remove write
// access for other users while keeping private keys unreadable to non-admins.

// postinstallEnv runs the script against a throwaway tree. `chown` is stubbed:
// the installer runs as root and this does not, and what these tests are about
// is the permission bits — who may write a file — rather than which uid owns
// it. Everything else is the real tool.
func postinstallEnv(t *testing.T, prefix string) []string {
	t.Helper()
	stubs := filepath.Join(prefix, "stubs")
	mkdirAll(t, stubs)
	writeExec(t, filepath.Join(stubs, "chown"), "#!/bin/sh\n# Ownership needs root; these tests check the mode bits.\nexit 0\n")
	return []string{
		"PATH=" + stubs + ":/usr/bin:/bin:/usr/sbin:/sbin",
		"JORBIQUE_VPN_PKG_PREFIX=" + prefix,
	}
}

// runPostinstall lays the payload down under a temporary prefix and runs the
// script against it, the way the installer would against /.
func runPostinstall(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	prefix := t.TempDir()
	libexec := filepath.Join(prefix, "usr/local/libexec/jorbique-vpn")
	cfg := filepath.Join(prefix, "Library/Application Support/JorbiqueVPN/cfg")
	mkdirAll(t, libexec, cfg)

	// The payload as the package delivers it, plus the profiles a machine being
	// upgraded already has. install-service is the client's own job; here it
	// only has to succeed so the script reaches the end.
	writeExec(t, filepath.Join(libexec, "JorbiqueVPN"), "#!/bin/sh\nexit 0\n")
	writeExec(t, filepath.Join(libexec, "sing-box"), "#!/bin/sh\nexit 0\n")
	writeExec(t, filepath.Join(libexec, "jorbique-vpn-admin"), "#!/bin/sh\nexit 0\n")
	// Group-writable going in: this is a machine upgraded from the version that
	// made them so, and the postinstall is what has to put them back.
	for _, name := range []string{"singbox_client.json", "singbox.json", "vpn_cfg.json"} {
		p := filepath.Join(cfg, name)
		writeFile(t, p, "client\n")
		if err := os.Chmod(p, 0o664); err != nil {
			t.Fatal(err)
		}
	}

	cmd := exec.Command("sh", "pkg/postinstall")
	cmd.Env = postinstallEnv(t, prefix)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("postinstall failed: %v\n%s", err, out)
	}
	return prefix
}

// Root alone can change the configuration the privileged engine consumes.
func TestPostinstallLeavesNoGroupWritableProfiles(t *testing.T) {
	prefix := runPostinstall(t)
	cfg := filepath.Join(prefix, "Library/Application Support/JorbiqueVPN/cfg")

	entries, err := os.ReadDir(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("the fixture left no files to check")
	}
	for _, e := range entries {
		st, err := os.Stat(filepath.Join(cfg, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm()&0o022 != 0 {
			t.Errorf("%s is %04o: writable outside root",
				e.Name(), st.Mode().Perm())
		}
	}
}

// The same rule over the whole tree the daemon reads, not only the directory
// the finding named. A recursive g+w anywhere under here reintroduces it.
func TestPostinstallLeavesNothingUnderTheDataDirsGroupWritable(t *testing.T) {
	prefix := runPostinstall(t)

	for _, dir := range []string{
		"Library/Application Support/JorbiqueVPN",
		"Library/Logs/JorbiqueVPN",
		"Library/Caches/JorbiqueVPN",
		"usr/local/libexec/jorbique-vpn",
	} {
		root := filepath.Join(prefix, dir)
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return nil
			}
			if info.Mode().Perm()&0o022 != 0 {
				rel, _ := filepath.Rel(prefix, path)
				t.Errorf("%s is %04o: writable by someone who is not root", rel, info.Mode().Perm())
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", dir, err)
		}
	}
}

// The config directory is readable by the admin group on purpose — the menu bar
// runs as the user and shows what is configured — and by nobody else, because
// the profiles in it carry private keys.
func TestPostinstallKeepsProfilesOffLimitsToOtherUsers(t *testing.T) {
	prefix := runPostinstall(t)
	cfg := filepath.Join(prefix, "Library/Application Support/JorbiqueVPN/cfg")

	st, err := os.Stat(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm()&0o007 != 0 {
		t.Errorf("the config directory is %04o: readable by any user on the machine", st.Mode().Perm())
	}
	profile, err := os.Stat(filepath.Join(cfg, "singbox.json"))
	if err != nil {
		t.Fatal(err)
	}
	if profile.Mode().Perm()&0o007 != 0 {
		t.Errorf("singbox.json is %04o: its private key is readable by any user", profile.Mode().Perm())
	}
}

// The prefix is a test seam, and a seam that silently became the default would
// point a real install at a directory nobody looks in. Unset means the machine.
func TestPostinstallTargetsTheMachineWithoutAPrefix(t *testing.T) {
	body, err := os.ReadFile("pkg/postinstall")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `PREFIX="${JORBIQUE_VPN_PKG_PREFIX:-}"`) {
		t.Error("the prefix no longer defaults to empty, so a real install would not target /")
	}
}

// A new app sits in /Applications for a minute or two before Spotlight indexes
// it, so the installer asks rather than waits. Both calls are for the machine's
// own indexes, which a prefixed (test) install must not touch.
func TestPostinstallNudgesTheIndexesOnlyForARealInstall(t *testing.T) {
	body, err := os.ReadFile("pkg/postinstall")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)

	for _, call := range []string{"/usr/bin/mdimport", "lsregister"} {
		if !strings.Contains(script, call) {
			t.Errorf("postinstall never calls %s, so a fresh install stays out of search until Spotlight gets to it", call)
		}
	}

	// The guard has to be on the same block: without it a prefixed run would
	// reach past its own tree into the machine's Launch Services database.
	guard := strings.Index(script, `if [ -z "$PREFIX" ] && [ -d "/Applications/Jorbique VPN.app" ]; then`)
	if guard < 0 {
		t.Fatal("the index calls are not guarded by an unprefixed-install check")
	}
	if idx := strings.Index(script, "/usr/bin/mdimport"); idx < guard {
		t.Error("mdimport runs before the guard that keeps a prefixed install off the machine")
	}
}
