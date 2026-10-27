package vpnmgr

import (
	"os"
	"path/filepath"
	"testing"

	"jorbiquevpn/internal/macos/core"
)

// Clear logs empties root's logs in place: sing-box and launchd hold them open,
// and a removed file would take their next writes with it.
func TestClearLogsEmptiesLogsInPlaceAndLeavesTheRest(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{Paths: &core.Paths{SystemLogDir: dir}}
	files := map[string]string{"app.log": "daemon\n", "Jorbique VPN.log": "core\n", "singbox.json": "{}"}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "old.log"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := m.ClearLogs(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"app.log", "Jorbique VPN.log"} {
		st, err := os.Stat(filepath.Join(dir, name))
		if err != nil || st.Size() != 0 {
			t.Errorf("%s should still exist and be empty: %v", name, err)
		}
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "singbox.json")); string(data) != "{}" {
		t.Error("a file that is not a log was touched")
	}
}
