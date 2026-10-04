package vpnmgr

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/telemetry"
)

// testUserUUID is the account every test manager that starts tunnels has: none
// starts without one.
const testUserUUID = "7998568d-7ae4-4f65-a4cb-b20e78af6a87"

// No test reaches the server: a UUID counts as known unless a test says otherwise.
func TestMain(m *testing.M) {
	verifyUserUUID = func(context.Context, string, string, func() map[string]bool, func() string) error { return nil }
	os.Exit(m.Run())
}

// answerUUIDChecks makes the server answer err and returns the UUIDs it was asked about.
func answerUUIDChecks(t *testing.T, err error) *[]string {
	t.Helper()
	asked := &[]string{}
	old := verifyUserUUID
	verifyUserUUID = func(_ context.Context, _ string, uuid string, _ func() map[string]bool, _ func() string) error {
		*asked = append(*asked, uuid)
		return err
	}
	t.Cleanup(func() { verifyUserUUID = old })
	return asked
}

func TestSetSingboxConfigStoresOnlyAUUIDTheServerKnows(t *testing.T) {
	const uuid = "7998568d-7ae4-4f65-a4cb-b20e78af6a87"
	for _, tc := range []struct {
		name   string
		server error
		want   string
	}{
		{"unknown", telemetry.ErrUnknownUUID, "not registered on the server"},
		{"unreachable", errors.New("dial tcp: connection refused"), "Could not reach the server"},
		{"verification unavailable", telemetry.ErrVerificationUnavailable, "server does not support UUID verification"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newFirstRunManager(t)
			before, _ := os.ReadFile(m.Paths.CfgFile)
			asked := answerUUIDChecks(t, tc.server)

			err := m.SetSingboxConfig([]string{uuid, ""})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("SetSingboxConfig = %v, want %q", err, tc.want)
			}
			if len(*asked) != 1 || (*asked)[0] != uuid {
				t.Fatalf("server asked about %q", *asked)
			}
			if after, _ := os.ReadFile(m.Paths.CfgFile); string(after) != string(before) {
				t.Fatalf("a refused UUID changed the config:\n%s", after)
			}
		})
	}
}

// Only a UUID that changes is asked about: the stack panel sends the stored
// one back with every stack change, and an empty first line keeps it.
func TestSetSingboxConfigDoesNotAskAboutTheStoredUUID(t *testing.T) {
	m := newFirstRunManager(t)
	m.Cfg.Singbox.UUID = testUserUUID
	asked := answerUUIDChecks(t, telemetry.ErrUnknownUUID)
	for _, first := range []string{"7998568D-7AE4-4F65-A4CB-B20E78AF6A87", ""} {
		if err := m.SetSingboxConfig([]string{first, ""}); err != nil && strings.Contains(err.Error(), "registered") {
			t.Fatalf("%q was checked again: %v", first, err)
		}
	}
	if len(*asked) != 0 {
		t.Fatalf("server asked about %q", *asked)
	}
}

// Until the first-run window has stored a UUID nothing brings a tunnel up: not
// the daemon's autostart and healing, not `start` in a terminal.
func TestNoTunnelStartsWithoutAUserUUID(t *testing.T) {
	v := failsToStart("no-uuid")
	m := testManager(t, v)
	m.Cfg.Singbox.UUID = ""
	m.saveStartedList([]string{v.Name})

	if err := m.AutostartCheck(true, false); err != nil {
		t.Fatalf("AutostartCheck: %v", err)
	}
	if log := covMgrLog(t, m); strings.Contains(log, "force restart") {
		t.Errorf("the autostart check restarted a tunnel with no UUID stored:\n%s", log)
	}
	for name, run := range map[string]func() error{
		"start":   func() error { return m.startTargets([]core.VPNItem{v}) },
		"restart": func() error { return m.restartTargets([]core.VPNItem{v}) },
	} {
		if err := run(); err == nil || !strings.Contains(err.Error(), "No user UUID") {
			t.Errorf("%s without a UUID = %v", name, err)
		}
	}
}
