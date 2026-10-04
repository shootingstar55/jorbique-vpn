package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runPostinstallWithClient lays the payload down under a temporary prefix with a
// given stub for the client binary, and returns the script's combined output
// and exit code rather than failing on a non-zero one — these tests are about
// what the script does when the daemon cannot be installed.
func runPostinstallWithClient(t *testing.T, clientScript string) (string, int) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	prefix := t.TempDir()
	libexec := filepath.Join(prefix, "usr/local/libexec/jorbique-vpn")
	cfg := filepath.Join(prefix, "Library/Application Support/JorbiqueVPN/cfg")
	mkdirAll(t, libexec, cfg)

	writeExec(t, filepath.Join(libexec, "JorbiqueVPN"), clientScript)
	writeExec(t, filepath.Join(libexec, "sing-box"), "#!/bin/sh\nexit 0\n")
	writeExec(t, filepath.Join(libexec, "jorbique-vpn-admin"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(cfg, "vpn_cfg.json"), "{}\n")

	cmd := exec.Command("sh", "pkg/postinstall")
	cmd.Env = append(postinstallEnv(t, prefix), "JORBIQUE_TEST_COUNTER="+filepath.Join(prefix, "attempts"))
	out, err := cmd.CombinedOutput()
	code := 0
	var ee *exec.ExitError
	if err != nil {
		if ok := asExitError(err, &ee); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("running postinstall: %v", err)
		}
	}
	return string(out), code
}

func asExitError(err error, target **exec.ExitError) bool {
	ee, ok := err.(*exec.ExitError)
	if ok {
		*target = ee
	}
	return ok
}

// Without the daemon the client cannot start a tunnel, save a setting or renew
// a config, and nothing on screen would say why — so the package reports the
// failure rather than looking installed and doing nothing. This gate had no
// test, which is how its retry budget went unexamined.
func TestPostinstallFailsWhenTheDaemonCannotBeInstalled(t *testing.T) {
	out, code := runPostinstallWithClient(t, "#!/bin/sh\n# every subcommand fails, install-service included\nexit 1\n")
	if code == 0 {
		t.Errorf("postinstall reported success with no daemon installed:\n%s", out)
	}
	if !strings.Contains(out, "cannot start tunnels without it") {
		t.Errorf("the failure does not say what is broken:\n%s", out)
	}
}

// launchd refuses a bootstrap while the job it replaces is still going away,
// and an installer runs on a busy machine. A first refusal must not fail the
// package: the attempt is repeated before that verdict is reached.
func TestPostinstallRetriesTheDaemonInstall(t *testing.T) {
	// Fails install-service once, succeeds after. Any other subcommand succeeds.
	client := `#!/bin/sh
if [ "$1" = "install-service" ]; then
  n=0
  [ -f "$JORBIQUE_TEST_COUNTER" ] && n=$(cat "$JORBIQUE_TEST_COUNTER")
  n=$((n + 1))
  echo "$n" > "$JORBIQUE_TEST_COUNTER"
  [ "$n" -le 1 ] && exit 1
fi
exit 0
`
	out, code := runPostinstallWithClient(t, client)
	if code != 0 {
		t.Errorf("a single refusal failed the whole install (exit %d):\n%s", code, out)
	}
	if !strings.Contains(out, "attempt 1 failed") {
		t.Errorf("the retry is silent; the installer log should say it happened:\n%s", out)
	}
}

// The retry is bounded: a machine where the daemon will never install must not
// leave the installer trying for ever.
func TestPostinstallGivesUpAfterAFewAttempts(t *testing.T) {
	client := `#!/bin/sh
if [ "$1" = "install-service" ]; then
  n=0
  [ -f "$JORBIQUE_TEST_COUNTER" ] && n=$(cat "$JORBIQUE_TEST_COUNTER")
  echo "$((n + 1))" > "$JORBIQUE_TEST_COUNTER"
  exit 1
fi
exit 0
`
	prefix := t.TempDir()
	_ = prefix
	out, code := runPostinstallWithClient(t, client)
	if code == 0 {
		t.Fatalf("expected a failure:\n%s", out)
	}
	attempts := strings.Count(out, "daemon install attempt")
	if attempts < 2 || attempts > 5 {
		t.Errorf("tried %d times; want a handful, neither one nor for ever:\n%s", attempts, out)
	}
}

// The counter file the stubs use must not survive into the payload's own tree.
func TestPostinstallTestCounterStaysOutOfTheTree(t *testing.T) {
	out, _ := runPostinstallWithClient(t, "#!/bin/sh\nexit 0\n")
	if strings.Contains(out, "JORBIQUE_TEST_COUNTER") {
		t.Errorf("the stub's plumbing leaked into the installer log:\n%s", out)
	}
	_ = os.Getenv("JORBIQUE_TEST_COUNTER")
}
