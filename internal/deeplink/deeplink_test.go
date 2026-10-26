package deeplink

import (
	"os"
	"path/filepath"
	"testing"
)

const sample = "0b5e0f3a-1c2d-4e5f-8a9b-0c1d2e3f4a5b"

func TestImportUUID(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want string
	}{
		{"jorbique://import?uuid=" + sample, sample},
		{"JORBIQUE://Import?uuid=0B5E0F3A-1C2D-4E5F-8A9B-0C1D2E3F4A5B", sample},
		{"jorbique://import/?uuid=" + sample, sample},
		{"jorbique:///import?uuid=" + sample, sample},
		{"jorbique:import?uuid=" + sample, sample},
		{"  jorbique://import?uuid=" + sample + "&from=sub  ", sample},
	} {
		got, ok := ImportUUID(test.raw)
		if !ok || got != test.want {
			t.Errorf("ImportUUID(%q) = %q, %v; want %q", test.raw, got, ok, test.want)
		}
	}
	for _, raw := range []string{
		"",
		sample,
		"https://import?uuid=" + sample,
		"jorbique://connect?uuid=" + sample,
		"jorbique://import",
		"jorbique://import?uuid=not-a-uuid",
		"jorbique://import?uuid=" + sample + "x",
		"jorbique://import?id=" + sample,
	} {
		if got, ok := ImportUUID(raw); ok {
			t.Errorf("ImportUUID(%q) = %q, want rejected", raw, got)
		}
	}
}

func TestFromArgsAndHasLink(t *testing.T) {
	args := []string{"--logon-autostart", "jorbique://import?uuid=" + sample}
	if got, ok := FromArgs(args); !ok || got != sample {
		t.Fatalf("FromArgs = %q, %v", got, ok)
	}
	if _, ok := FromArgs([]string{"--logon-autostart"}); ok {
		t.Fatal("FromArgs found a link in plain arguments")
	}
	if !HasLink([]string{"jorbique://import?uuid=bad"}) || HasLink([]string{"--menu-bar"}) {
		t.Fatal("HasLink misread its arguments")
	}
}

func TestPendingIsTakenOnce(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if HasPending(dir) || TakePending(dir) != "" {
		t.Fatal("empty dir has a pending link")
	}
	if err := SavePending(dir, sample); err != nil {
		t.Fatal(err)
	}
	if !HasPending(dir) {
		t.Fatal("saved link is not pending")
	}
	if got := TakePending(dir); got != sample {
		t.Fatalf("TakePending = %q", got)
	}
	if HasPending(dir) || TakePending(dir) != "" {
		t.Fatal("link was not removed after it was taken")
	}
	if err := os.WriteFile(filepath.Join(dir, pendingFile), []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := TakePending(dir); got != "" {
		t.Fatalf("TakePending returned %q for garbage", got)
	}
}
