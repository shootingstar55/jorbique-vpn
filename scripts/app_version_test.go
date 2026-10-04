// Package scripts holds tests for the repository's build scripts. It carries no
// Go source of its own — app-version.sh decides the version every build stamps
// into Windows resources and Info.plist, and that contract is worth a test.
package scripts

import (
	"errors"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

var bareVersion = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// runResolver runs app-version.sh with exactly the given environment — never the
// caller's, whose APP_VERSION would decide the outcome for us.
func runResolver(t *testing.T, env ...string) (string, int) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	cmd := exec.Command("bash", "app-version.sh")
	cmd.Env = append([]string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/tmp"}, env...)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("running app-version.sh: %v", err)
		}
		code = ee.ExitCode()
	}
	return strings.TrimSpace(string(out)), code
}

// The version reaches the PE version resource and Info.plist verbatim, so every
// path must yield a bare x.y.z — including the APP_VERSION override, which used
// to be printed back unchecked.
func TestAppVersionRejectsMalformedOverride(t *testing.T) {
	for _, bad := range []string{
		"not-a-version",
		"2.9.1-12-gdeadbee", // a git describe string
		"v2.9",
		"2.9.1 ",
		"; rm -rf /",
	} {
		out, code := runResolver(t, "APP_VERSION="+bad)
		if code == 0 {
			t.Errorf("APP_VERSION=%q was accepted, printed %q", bad, out)
		}
		if !strings.Contains(out, "not a bare x.y.z") {
			t.Errorf("APP_VERSION=%q: unhelpful failure %q", bad, out)
		}
	}
}

func TestAppVersionAcceptsOverride(t *testing.T) {
	for in, want := range map[string]string{
		"3.1.4":  "3.1.4",
		"v3.1.4": "3.1.4", // the v is stripped, as for tags
	} {
		out, code := runResolver(t, "APP_VERSION="+in)
		if code != 0 || out != want {
			t.Errorf("APP_VERSION=%q -> %q (exit %d), want %q", in, out, code, want)
		}
	}
}

// Without an override the version is the VERSION file. The Makefile's
// APP_VERSION=0.0.0-dev default means "no override" too.
func TestAppVersionReadsTheVersionFile(t *testing.T) {
	data, err := os.ReadFile("../VERSION")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSpace(string(data))
	if !bareVersion.MatchString(want) {
		t.Fatalf("VERSION = %q, want a bare x.y.z", want)
	}
	for _, env := range [][]string{nil, {"APP_VERSION=0.0.0-dev"}} {
		if out, code := runResolver(t, env...); code != 0 || out != want {
			t.Errorf("%v -> %q (exit %d), want %q", env, out, code, want)
		}
	}
}
