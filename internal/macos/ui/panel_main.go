//go:build darwin

package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"jorbiquevpn/internal/autoconnect"
	"jorbiquevpn/internal/ipinfo"
	"jorbiquevpn/internal/localapi"
	"jorbiquevpn/internal/macos/core"
)

// mainState is what the main window shows. It is the same information the menu
// bar renders, read the same way — `status` on the CLI — so the two cannot
// disagree about whether a tunnel is up.
type mainState struct {
	VPNs []string `json:"vpns"`
	// The protocols the downloaded profile offers, for the line under the app
	// name — the same one the Windows header shows.
	Outbounds []string `json:"outbounds"`
	Running   []string `json:"running"`
	ExitIP    string   `json:"exitIP"`
	Daemon    string   `json:"daemon"`
	// Action is what the bar is doing right now — "Connecting", "Disconnecting",
	// "Restarting" — so the window shows the same in-flight state the status
	// glyph animates through, instead of jumping from idle straight to connected.
	Action             string               `json:"action"`
	ConnectionProgress autoconnect.Progress `json:"connectionProgress"`
	// Report is the last connection check while a tunnel is up: every
	// protocol it saw, and which one the tunnel runs on. Null when off.
	Report       *autoconnect.Report `json:"report"`
	VPNAutostart bool                `json:"vpnAutostart"`
	AppAutostart bool                `json:"appAutostart"`
}

// mainAction is one press in the window: the connect ring, a switch, or a button
// that opens one of the other panels.
type mainAction struct {
	Action string          `json:"action"`
	Value  json.RawMessage `json:"value"`
}

// showMainPanel opens the main window.
func showMainPanel(state func() mainState, act func(mainAction) error, ipInfo func(force bool) (ipinfo.Info, error), onClosed func()) (*webPanel, error) {
	assets, err := frontendAssets()
	if err != nil {
		return nil, err
	}
	panel, err := showWebPanel("Jorbique VPN Extended", 560, 880, 440, 560,
		func(closeWindow func()) (*localapi.LocalServer, error) {
			return localapi.StartFormServerFunc(panelQuery("main"), "main", assets,
				func() any { return state() },
				func(body json.RawMessage) error {
					var action mainAction
					if err := json.Unmarshal(body, &action); err != nil {
						return fmt.Errorf("invalid request")
					}
					if act == nil {
						return nil
					}
					return act(action)
				},
				// The lookup is an action of its own rather than part of the
				// status the window polls every second: it reaches a public
				// service over the tunnel, and the card asks for it on its own
				// schedule — every few minutes, or when the user presses refresh.
				map[string]func(json.RawMessage) (any, error){
					"ipinfo": func(body json.RawMessage) (any, error) {
						var request struct {
							Force bool `json:"force"`
						}
						_ = json.Unmarshal(body, &request)
						if ipInfo == nil {
							return ipinfo.Info{}, nil
						}
						return ipInfo(request.Force)
					},
				},
				closeWindow)
		}, onClosed)
	if err != nil {
		return nil, err
	}
	// The main window is the app, not a palette over it: it minimises to the
	// Dock and does not sit above every other window on the screen.
	panel.SetNormalLevel()
	return panel, nil
}

// mainStateOf renders the parsed status into what the window draws.
func mainStateOf(cfg *core.AppConfig, paths *core.Paths, ms MenuStatus, running []string, exitIP string) mainState {
	// Empty slices, never nil: a nil slice encodes as JSON null, and the page
	// reads .length off both of these. One status call that exited non-zero was
	// enough to send null and blank the whole hero card, ring included.
	// Every protocol, Naive included: each connect checks Naive first, so it
	// belongs in the list whether or not the tunnel ends up on it.
	outbounds := append([]string{}, singboxProfileProtocols(cfg)...)
	state := mainState{
		VPNs:      []string{},
		Outbounds: outbounds,
		Running:   []string{},
		ExitIP:    exitIP,
		Daemon:    daemonLabel(ms.SudoersLine),
	}
	// The connection check runs in the root daemon and leaves its files in the
	// daemon's run directory, not in this user's own one (paths.RunDir).
	daemonRunDir := core.DaemonRunDir()
	if paths != nil {
		if data, err := os.ReadFile(filepath.Join(daemonRunDir, autoconnect.ProgressFile)); err == nil {
			_ = json.Unmarshal(data, &state.ConnectionProgress)
		}
	}
	// The window and the menu control the same sing-box connection.
	singbox := map[string]bool{}
	for _, vpn := range availableVPNs(cfg, paths) {
		if vpn.Type == string(core.VPNSingbox) {
			singbox[vpn.Name] = true
		}
	}
	for _, name := range running {
		if singbox[name] {
			state.Running = append(state.Running, name)
		}
	}
	if len(state.Running) > 0 {
		if report, ok := autoconnect.ReadReport(daemonRunDir); ok {
			state.Report = &report
		}
	}
	for _, vpn := range availableVPNs(cfg, paths) {
		state.VPNs = append(state.VPNs, vpn.Name)
	}
	// The switches read the same parse the menu footer used to paint green or
	// red, rather than matching the phrasing a second time.
	autostart := computeAutostartState(ms)
	state.VPNAutostart = autostart.vpnOK
	state.AppAutostart = autostart.barOK
	return state
}

// boolValue reads a switch's new position out of an action.
func boolValue(raw json.RawMessage) bool {
	var on bool
	_ = json.Unmarshal(raw, &on)
	return on
}

// stringValue reads which panel a button asked for.
func stringValue(raw json.RawMessage) string {
	var value string
	_ = json.Unmarshal(raw, &value)
	return value
}
