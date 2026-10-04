//go:build darwin

package ui

import (
	"strings"
	"testing"
)

// A privileged action with no daemon to run it fails, the menu goes back to
// "everything stopped", and the click looks ignored. The one thing the user can
// act on is knowing the service is gone, so the phrase the client prints has to
// keep matching what the bar looks for.
func TestTheDaemonMissingMessageIsRecognised(t *testing.T) {
	printed := "start needs the Jorbique VPN daemon, which is not running. Reinstall the client, or start it with: …"
	if !strings.Contains(printed, daemonMissingMarker) {
		t.Errorf("the bar would not recognise %q", printed)
	}
	for _, other := range []string{
		"Already stopped Jorbique VPN",
		"privileged daemon: running (/var/run/jorbique-vpn/control.sock)",
	} {
		if strings.Contains(other, daemonMissingMarker) {
			t.Errorf("ordinary output %q reads as a missing daemon", other)
		}
	}
}
