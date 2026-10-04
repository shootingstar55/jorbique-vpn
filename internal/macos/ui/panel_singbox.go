//go:build darwin

package ui

import (
	"fmt"
	"strings"

	"jorbiquevpn/internal/dashboard"
)

// showSingboxConfigPanel presents the VPN configuration form: the UUID and a
// switch per protocol the profile offers.
func showSingboxConfigPanel(initialUUID string, initialEnabled, available []string, onSubmit func(string, []string) error, onClosed func()) (*webPanel, error) {
	assets, err := frontendAssets()
	if err != nil {
		return nil, err
	}
	return showWebPanel("VPN configuration", 780, 620, 640, 480, func(closeWindow func()) (*dashboard.LocalServer, error) {
		return dashboard.StartConfigServer(
			panelQuery("vpn-config"),
			singboxConfigState(initialUUID, initialEnabled, available),
			assets,
			func(update dashboard.ConfigUpdate) error {
				uuid, enabled, err := singboxConfigSelection(update, available)
				if err != nil {
					return err
				}
				if onSubmit == nil {
					return nil
				}
				return onSubmit(uuid, enabled)
			},
			closeWindow,
		)
	}, onClosed)
}

func singboxConfigSelection(update dashboard.ConfigUpdate, available []string) (string, []string, error) {
	uuid := strings.TrimSpace(update.UUID)
	if uuid == "" {
		return "", nil, fmt.Errorf("Enter a user UUID")
	}
	if !validSingboxUUID(uuid) {
		return "", nil, fmt.Errorf("Invalid user UUID format")
	}
	var enabled []string
	for _, tag := range available {
		if on, answered := update.OutboundEnabled[tag]; !answered || on {
			enabled = append(enabled, tag)
		}
	}
	return uuid, enabled, nil
}

func singboxConfigState(initialUUID string, initialEnabled, available []string) dashboard.ConfigState {
	enabledSet := lowerStringSet(initialEnabled)
	state := dashboard.ConfigState{
		UUID:               initialUUID,
		AvailableOutbounds: append([]string(nil), available...),
		OutboundEnabled:    make(map[string]bool, len(available)),
	}
	for _, tag := range available {
		// The list says what is on, and an empty one means nothing is. It used
		// to mean "no opinion, tick everything", which drew every protocol back
		// on for a client that had switched them all off.
		state.OutboundEnabled[tag] = enabledSet[strings.ToLower(tag)]
	}
	return state
}

// singboxStdin builds the stdin payload for `set-singbox-config`: the UUID and
// the enabled outbounds, one line each.
func singboxStdin(uuid string, enabled []string) string {
	return uuid + "\n" + strings.Join(enabled, ",") + "\n"
}

func lowerStringSet(in []string) map[string]bool {
	out := make(map[string]bool, len(in))
	for _, value := range in {
		out[strings.ToLower(value)] = true
	}
	return out
}
