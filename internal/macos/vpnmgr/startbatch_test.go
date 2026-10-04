package vpnmgr

import (
	"errors"
	"testing"

	"jorbiquevpn/internal/macos/core"
)

func testCore(name string) core.VPNItem {
	return core.VPNItem{Name: name, Type: core.VPNSingbox, File: name + ".json"}
}

// The report: starting everything stopped at the first VPN that did not come
// up. A profile waiting on an approval the user never gave took every VPN
// behind it down with it — and the client called that a start.
func TestOneFailureDoesNotCancelTheRest(t *testing.T) {
	targets := []core.VPNItem{testCore("intra"), testCore("projects"), testCore("corp")}

	var attempted []string
	started, _, failures := startBatchWith(targets, func(v core.VPNItem) error {
		attempted = append(attempted, v.Name)
		if v.Name == "intra" {
			return errors.New("did not initialize")
		}
		return nil
	})

	if len(attempted) != len(targets) {
		t.Fatalf("tried %v, wanted every target", attempted)
	}
	if len(started) != 2 || started[0].Name != "corp" || started[1].Name != "projects" {
		t.Errorf("started = %v, wanted the two that came up", names(started))
	}
	if len(failures) != 1 || failures[0] != "intra: did not initialize" {
		t.Errorf("failures = %v", failures)
	}
}

func names(items []core.VPNItem) []string {
	out := make([]string, 0, len(items))
	for _, v := range items {
		out = append(out, v.Name)
	}
	return out
}

// Losing the start lock means another process is bringing this tunnel up. The
// batch used to read that as success and put the VPN on the started list — what
// an autostart restores at boot — on the strength of a start it never watched
// finish. It is not a failure either: failing the batch would fail a start that
// is very likely on its way.
func TestALostStartRaceIsNeitherStartedNorFailed(t *testing.T) {
	targets := []core.VPNItem{testCore("intra"), testCore("projects"), testCore("corp")}

	started, skipped, failures := startBatchWith(targets, func(v core.VPNItem) error {
		switch v.Name {
		case "intra":
			return errStartInProgress
		case "projects":
			return errors.New("did not initialize")
		}
		return nil
	})

	if len(started) != 1 || started[0].Name != "corp" {
		t.Errorf("started = %v, want only the one this run brought up", names(started))
	}
	if len(failures) != 1 || failures[0] != "projects: did not initialize" {
		t.Errorf("failures = %v, want only the real failure", failures)
	}
	// Reported separately so the caller can keep it on the started list without
	// claiming this run brought it up — see TestARacedStartStaysOnTheStartedList.
	if len(skipped) != 1 || skipped[0].Name != "intra" {
		t.Errorf("skipped = %v, want the one another process was already starting", names(skipped))
	}
}
