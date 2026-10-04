package network

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStatePersistRoundTrip(t *testing.T) {
	dir := t.TempDir()

	if LoadState(dir) {
		t.Error("LoadState should report absent state")
	}
	SaveState(dir)
	if !LoadState(dir) {
		t.Error("LoadState failed after SaveState")
	}
	ClearState(dir)
	if LoadState(dir) {
		t.Error("ClearState did not remove state file")
	}
}

// A state file written before sing-box took the routes is still a valid marker;
// the route arrays it carries are simply not read any more.
func TestLoadState_StateFromOlderBuildsStillMarksATunnel(t *testing.T) {
	dir := t.TempDir()
	older := `{"added_routes":[{"Network":"0.0.0.0","Gateway":"100.64.0.1"}],` +
		`"original_routes":[{"Network":"0.0.0.0","Gateway":"192.168.0.1"}],"openvpn_routes":[]}`
	if err := os.WriteFile(filepath.Join(dir, stateFileName), []byte(older), 0644); err != nil {
		t.Fatal(err)
	}
	if !LoadState(dir) {
		t.Error("a state file from an older build should still load")
	}
}

func TestGetTunInterfaceNameFromConfig(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cfg.json")
	if err := os.WriteFile(p, []byte(`{"inbounds":[{"interface_name":" singtun7 "}]}`), 0644); err != nil {
		t.Fatal(err)
	}
	if got := GetTunInterfaceNameFromConfig(p); got != "singtun7" {
		t.Errorf("interface name = %q", got)
	}
	// Missing file / no inbounds -> "".
	if GetTunInterfaceNameFromConfig(filepath.Join(dir, "nope.json")) != "" {
		t.Error("missing file should yield empty")
	}
	_ = os.WriteFile(p, []byte(`{"inbounds":[]}`), 0644)
	if GetTunInterfaceNameFromConfig(p) != "" {
		t.Error("empty inbounds should yield empty")
	}
}

// On non-Windows the native adapter/route lookups are stubs returning "not found",
// so these read-only checks resolve quickly without touching the OS routing table.
func TestFindTunInterface_NotFoundOnStub(t *testing.T) {
	if _, _, ok := FindTunInterface("singtun0", ""); ok {
		t.Error("expected no TUN interface via stub")
	}
}
