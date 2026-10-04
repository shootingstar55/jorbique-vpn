package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageUpgradeStopsOldClientBeforeReplacingIt(t *testing.T) {
	{
		{
			prefix := t.TempDir()
			libexec := filepath.Join(prefix, "usr/local/libexec/jorbique-vpn")
			support := filepath.Join(prefix, "Library/Application Support/JorbiqueVPN")
			cfg := filepath.Join(support, "cfg/vpn_cfg.json")
			list := filepath.Join(support, "started.list")
			client := filepath.Join(libexec, "JorbiqueVPN")
			mkdirAll(t, filepath.Dir(cfg), filepath.Dir(list), filepath.Dir(client))
			writeFile(t, cfg, `{"singbox":{"uuid":"user-uuid"}}`)
			writeFile(t, list, "Jorbique VPN\n")
			writeFile(t, filepath.Join(prefix, "expected-config"), `{"singbox":{"uuid":"user-uuid"}}`)
			writeFile(t, filepath.Join(prefix, "expected-list"), "Jorbique VPN\n")
			profile := filepath.Join(filepath.Dir(cfg), "singbox_client.json")
			writeFile(t, profile, "cached profile\n")
			writeExec(t, client, `#!/bin/sh
set -e
[ "$*" = "stop all" ]
prefix="$JORBIQUE_VPN_PKG_PREFIX"
cmp "$prefix/expected-config" "$prefix/private/tmp/jorbique-vpn-preserve-vpn_cfg.json"
cmp "$prefix/expected-list" "$prefix/private/tmp/jorbique-vpn-preserve-started.list"
printf '' > "$OLD_STARTED_LIST"
printf 'stop\n' > "$prefix/order"
`)
			env := append(postinstallEnv(t, prefix), "OLD_STARTED_LIST="+list)
			cmd := exec.Command("sh", "pkg/preinstall")
			cmd.Env = env
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("preinstall: %v\n%s", err, out)
			}
			if data, err := os.ReadFile(list); err != nil || len(data) != 0 {
				t.Fatalf("old client did not clear its list: %q, %v", data, err)
			}

			// Installer now replaces the old binary and ships its default config.
			mkdirAll(t, libexec, filepath.Join(support, "cfg"))
			writeFile(t, filepath.Join(support, "cfg/vpn_cfg.json"), "package defaults\n")
			writeExec(t, filepath.Join(libexec, "JorbiqueVPN"), `#!/bin/sh
set -e
[ "$*" = "install-service" ]
prefix="$JORBIQUE_VPN_PKG_PREFIX"
cmp "$prefix/expected-config" "$prefix/Library/Application Support/JorbiqueVPN/cfg/vpn_cfg.json"
cmp "$prefix/expected-list" "$prefix/Library/Application Support/JorbiqueVPN/started.list"
[ "$(cat "$prefix/order")" = "stop" ]
printf 'install-service\n' >> "$prefix/order"
`)
			cmd = exec.Command("sh", "pkg/postinstall")
			cmd.Env = env
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("postinstall: %v\n%s", err, out)
			}
			for path, want := range map[string]string{
				filepath.Join(prefix, "order"): "stop\ninstall-service\n",
				profile:                        "cached profile\n",
			} {
				if data, err := os.ReadFile(path); err != nil || string(data) != want {
					t.Errorf("%s = %q, %v; want %q", path, data, err, want)
				}
			}
		}
	}
}

func TestPreinstallRefusesUpgradeWhenOldClientCannotStop(t *testing.T) {
	prefix := t.TempDir()
	libexec := filepath.Join(prefix, "usr/local/libexec/jorbique-vpn")
	support := filepath.Join(prefix, "Library/Application Support/JorbiqueVPN")
	mkdirAll(t, libexec, filepath.Join(support, "cfg"))
	writeFile(t, filepath.Join(support, "cfg/vpn_cfg.json"), "user settings\n")
	writeFile(t, filepath.Join(support, "started.list"), "Jorbique VPN\n")
	client := "#!/bin/sh\nprintf '' > \"$JORBIQUE_VPN_PKG_PREFIX/Library/Application Support/JorbiqueVPN/started.list\"\nexit 17\n"
	writeExec(t, filepath.Join(libexec, "JorbiqueVPN"), client)
	cmd := exec.Command("sh", "pkg/preinstall")
	cmd.Env = postinstallEnv(t, prefix)
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "could not stop tunnels") {
		t.Fatalf("preinstall accepted a failed stop: %v\n%s", err, out)
	}
	for path, want := range map[string]string{
		filepath.Join(libexec, "JorbiqueVPN"):                                   client,
		filepath.Join(support, "cfg/vpn_cfg.json"):                              "user settings\n",
		filepath.Join(support, "started.list"):                                  "Jorbique VPN\n",
		filepath.Join(prefix, "private/tmp/jorbique-vpn-preserve-started.list"): "Jorbique VPN\n",
	} {
		if data, err := os.ReadFile(path); err != nil || string(data) != want {
			t.Errorf("%s = %q, %v; want %q", path, data, err, want)
		}
	}
}

func TestPreinstallFreshInstallNeedsNoOldClient(t *testing.T) {
	cmd := exec.Command("sh", "pkg/preinstall")
	cmd.Env = postinstallEnv(t, t.TempDir())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fresh preinstall: %v\n%s", err, out)
	}
}

func TestPreinstallStopsDaemonAfterTunnelsOnlyOnRealMachine(t *testing.T) {
	body, err := os.ReadFile("pkg/preinstall")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	stop := strings.Index(script, `"$CLIENT" stop all`)
	guard := strings.Index(script, `if [ -z "$PREFIX" ] && /bin/launchctl print system/one.jorbique.vpn`)
	bootout := strings.Index(script, `/bin/launchctl bootout system/one.jorbique.vpn`)
	if stop < 0 || guard < stop || bootout < guard {
		t.Fatal("daemon must stop after tunnel cleanup and only without a test prefix")
	}
}
