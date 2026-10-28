package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPostinstallRestartsOnlyLoadedUserInterfaces(t *testing.T) {
	data, err := os.ReadFile("pkg/postinstall")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(data), "# Restart loaded user interfaces only after installation succeeds:")
	if start < 0 {
		t.Fatal("missing UI restart step")
	}
	// Exercise the real final installer step with process/launchd boundaries stubbed.
	for _, test := range []struct {
		name, prefix, loaded, fail, want string
	}{
		{"upgrade", "", "501 502", "", "gui/501/one.jorbique.vpn.bar\ngui/502/one.jorbique.vpn.bar\n"},
		{"fresh", "", "", "", ""},
		{"prefixed-install", "/throwaway", "501 502", "", ""},
		{"restart-failure", "", "501", "501", "gui/501/one.jorbique.vpn.bar\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			ps, launchctl, calls, windows := filepath.Join(dir, "ps"), filepath.Join(dir, "launchctl"), filepath.Join(dir, "calls"), filepath.Join(dir, "windows")
			writeExec(t, ps, "#!/bin/sh\nprintf '0\\n501\\n501\\n502\\n600\\ninvalid\\n'\n")
			writeExec(t, launchctl, `#!/bin/sh
case "$1" in
  print)
    for uid in $LOADED; do [ "$2" = "gui/$uid/one.jorbique.vpn.bar" ] && exit 0; done
    exit 1 ;;
  kickstart)
    [ "$2" = "-k" ] || exit 2
    printf '%s\n' "$3" >> "$CALLS"
    [ "$3" = "gui/$FAIL/one.jorbique.vpn.bar" ] && exit 3
    exit 0 ;;
  asuser)
    [ "$3" = /usr/bin/sudo ] && [ "$4" = -H ] && [ "$5" = -u ] && [ "$6" = "#$2" ] || exit 4
    [ "$7" = /usr/bin/open ] && [ "$8" = -a ] && [ "$9" = "/Applications/Jorbique VPN Extended.app" ] || exit 5
    printf 'gui/%s/one.jorbique.vpn.bar\n' "$2" >> "$WINDOWS"
    exit 0 ;;
esac
exit 2
`)
			block := strings.NewReplacer("/bin/ps", ps, "/bin/launchctl", launchctl).Replace(string(data)[start:])
			cmd := exec.Command("sh", "-c", "set -e\n"+block)
			cmd.Env = []string{"PATH=/usr/bin:/bin", "PREFIX=" + test.prefix, "LOADED=" + test.loaded, "FAIL=" + test.fail, "CALLS=" + calls, "WINDOWS=" + windows}
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("restart step: %v\n%s", err, out)
			}
			got, err := os.ReadFile(calls)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if string(got) != test.want {
				t.Errorf("restart requests %q, want %q", got, test.want)
			}
			opened, err := os.ReadFile(windows)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			wantWindows := test.want
			if test.fail != "" {
				wantWindows = strings.ReplaceAll(wantWindows, "gui/"+test.fail+"/one.jorbique.vpn.bar\n", "")
			}
			if string(opened) != wantWindows || strings.Contains(string(out), "could not reopen") {
				t.Errorf("windows opened %q, want %q; output %q", opened, wantWindows, out)
			}
			if strings.Contains(string(out), "could not restart") != (test.fail != "") {
				t.Errorf("restart failure diagnostic: %q", out)
			}
		})
	}
}
