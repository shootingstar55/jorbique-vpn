//go:build darwin

package ui

import (
	"encoding/json"
	"strings"

	"jorbiquevpn/internal/dashboard"
	"jorbiquevpn/internal/macos/ansiutil"
)

// showUUIDGatePanel opens the first-run window (ActivationScreen.vue): until a
// UUID the server knows is stored it is the only thing the app shows.
func showUUIDGatePanel(onSubmit func(uuid string) error, onClosed func()) (*webPanel, error) {
	assets, err := frontendAssets()
	if err != nil {
		return nil, err
	}
	return showWebPanel("Jorbique VPN Extended", 520, 400, 440, 340, func(closeWindow func()) (*dashboard.LocalServer, error) {
		return dashboard.StartFormServerWithActions(panelQuery("activate"), "uuid", assets, struct{}{},
			func(raw json.RawMessage) error {
				var form struct {
					UUID string `json:"uuid"`
				}
				if err := json.Unmarshal(raw, &form); err != nil {
					return err
				}
				return onSubmit(strings.TrimSpace(form.UUID))
			},
			// The theme picked here is the one the windows after it open in.
			map[string]func(json.RawMessage) (any, error){
				"theme": func(raw json.RawMessage) (any, error) {
					writeThemeMode(stringValue(raw))
					return nil, nil
				},
			},
			closeWindow,
		)
	}, onClosed)
}

// uuidGateError is the line of a failed set-singbox-config that the window
// shows: the error the child reported, without the colour and the kind prefix
// ActivationScreen.vue has no use for. The child logs to the same stream
// before it fails — "[time] WARN: tool not found: sing-box" on every fresh
// install — so the last line that is neither a log line nor a hint is the one.
func uuidGateError(stderr string) string {
	reason := ""
	for _, line := range strings.Split(ansiutil.Strip(stderr), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[") || strings.HasPrefix(line, "Hint:") {
			continue
		}
		reason = line
	}
	if reason == "" {
		return "Could not save the user UUID"
	}
	return strings.TrimPrefix(reason, "Bad config: ")
}
