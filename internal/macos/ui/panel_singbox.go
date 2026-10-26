//go:build darwin

package ui

import (
	"fmt"
	"strings"

	"jorbiquevpn/internal/localapi"
)

// showUUIDEditPanel contains only the saved account UUID.
func showUUIDEditPanel(initialUUID string, onSubmit func(string) error, onClosed func()) (*webPanel, error) {
	assets, err := frontendAssets()
	if err != nil {
		return nil, err
	}
	return showWebPanel("UUID пользователя", 520, 300, 440, 260, func(closeWindow func()) (*localapi.LocalServer, error) {
		return localapi.StartConfigServer(panelQuery("set-uuid"), localapi.ConfigState{UUID: initialUUID}, assets,
			func(update localapi.ConfigUpdate) error {
				uuid := strings.TrimSpace(update.UUID)
				if !validSingboxUUID(uuid) {
					return fmt.Errorf("Enter a valid user UUID")
				}
				if onSubmit != nil {
					return onSubmit(uuid)
				}
				return nil
			}, closeWindow)
	}, onClosed)
}

func singboxConfigSelection(update localapi.ConfigUpdate, available []string) (string, []string, error) {
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

func singboxConfigState(initialUUID string, initialEnabled, available []string, naive bool) localapi.ConfigState {
	enabledSet := lowerStringSet(initialEnabled)
	state := localapi.ConfigState{
		UUID:               initialUUID,
		NaiveMode:          naive,
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

// singboxStdin builds the stdin payload for `set-singbox-config`: the UUID,
// the enabled outbounds, the selector node (blank: keep it) and the mode
// (blank: keep it), one line each.
func singboxStdin(uuid string, enabled []string, mode string) string {
	return uuid + "\n" + strings.Join(enabled, ",") + "\n\n" + mode + "\n"
}

// singboxModeLine is the mode line for a choice the page made, or blank when it
// made none.
func singboxModeLine(naive *bool) string {
	switch {
	case naive == nil:
		return ""
	case *naive:
		return "naive"
	}
	return "standard"
}

func lowerStringSet(in []string) map[string]bool {
	out := make(map[string]bool, len(in))
	for _, value := range in {
		out[strings.ToLower(value)] = true
	}
	return out
}
