package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"jorbiquevpn/internal/server"
	"jorbiquevpn/internal/telemetry"
)

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

func TestApp_ActivateUserUUID(t *testing.T) {
	for _, tc := range []struct {
		name   string
		input  string
		server error
		want   error
		asked  int
	}{
		{"not a UUID", "not-a-uuid", nil, ErrInvalidVless, 0},
		{"unknown to the server", appTestUUID, telemetry.ErrUnknownUUID, ErrUnknownUUID, 1},
		{"server unreachable", appTestUUID, errors.New("dial tcp: connection refused"), ErrServerUnreachable, 1},
		{"verification unavailable", appTestUUID, telemetry.ErrVerificationUnavailable, telemetry.ErrVerificationUnavailable, 1},
		{"accepted", " " + appTestUUID + " ", nil, nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp(t)
			a.store.SetUserUUIDAndOutbounds("", map[string]bool{"tuic": false})
			if err := a.SaveVpnFormDraft("", "draft.example", nil); err != nil {
				t.Fatal(err)
			}
			asked := answerUUIDChecks(t, tc.server)

			_, err := a.ActivateUserUUID(tc.input)
			if !errors.Is(err, tc.want) {
				t.Fatalf("ActivateUserUUID = %v, want %v", err, tc.want)
			}
			if len(*asked) != tc.asked {
				t.Fatalf("server asked %d times, want %d", len(*asked), tc.asked)
			}
			stored, _ := server.Load(a.dataDir)
			if tc.want != nil {
				if a.HasUserUUID() || stored.GetUserUUID() != "" {
					t.Fatalf("a refused UUID was stored: %q", stored.GetUserUUID())
				}
				return
			}
			if (*asked)[0] != appTestUUID || !a.HasUserUUID() || stored.GetUserUUID() != appTestUUID {
				t.Fatalf("accepted UUID: asked %q, stored %q", (*asked)[0], stored.GetUserUUID())
			}
			if stored.GetOutboundEnabled()["tuic"] {
				t.Error("activation reset the outbound choice")
			}
			if _, err := os.Stat(filepath.Join(a.dataDir, vpnFormDraftFile)); err == nil {
				t.Error("the stale draft outlived the activation")
			}
		})
	}
}

// Only a UUID the user changes is checked: the window saves the stored one
// again with every toggle, and that must not need the server.
func TestApp_SetVpnUserConfig_ChecksOnlyANewUUID(t *testing.T) {
	a := newTestApp(t)
	asked := answerUUIDChecks(t, nil)
	if _, err := a.SetVpnUserConfig(appTestUUID, allOutboundsEnabled()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetVpnUserConfig("  "+appTestUUID+"  ", map[string]bool{"tuic": false}); err != nil {
		t.Fatal(err)
	}
	if len(*asked) != 1 {
		t.Fatalf("server asked %d times, want once", len(*asked))
	}

	const other = "22222222-2222-2222-2222-222222222222"
	answerUUIDChecks(t, telemetry.ErrUnknownUUID)
	if _, err := a.SetVpnUserConfig(other, allOutboundsEnabled()); !errors.Is(err, ErrUnknownUUID) {
		t.Fatalf("unknown UUID: %v", err)
	}
	if got := a.store.GetUserUUID(); got != appTestUUID {
		t.Fatalf("an unknown UUID replaced the stored one: %q", got)
	}
	if _, err := a.SetVpnUserConfig("", map[string]bool{"proxy": false}); !errors.Is(err, ErrInvalidVless) {
		t.Fatalf("clearing the UUID: %v", err)
	}
}

func TestApp_ConnectRefusedWithoutUUID(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.Connect("system"); !errors.Is(err, ErrVlessRequired) {
		t.Fatalf("Connect without a UUID = %v, want ErrVlessRequired", err)
	}
}
