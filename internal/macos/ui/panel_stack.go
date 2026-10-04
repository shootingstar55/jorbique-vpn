//go:build darwin

package ui

import (
	"encoding/json"
	"fmt"
	"strings"

	"jorbiquevpn/internal/dashboard"
	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/macos/singbox"
)

// stackState is what the picker opens with, and stackUpdate is what comes back.
type stackState struct {
	Stack string `json:"stack"`
}

// showStackPanel presents the same stack cards the Windows client shows, served
// from the shared bundle.
func showStackPanel(current string, onPick func(string) error, onClosed func()) (*webPanel, error) {
	assets, err := frontendAssets()
	if err != nil {
		return nil, err
	}
	return showWebPanel("Select TUN mode", 520, 360, 420, 320,
		func(closeWindow func()) (*dashboard.LocalServer, error) {
			return dashboard.StartFormServer(panelQuery("stack"), "stack", assets, stackState{Stack: current},
				func(body json.RawMessage) error {
					var update stackState
					if err := json.Unmarshal(body, &update); err != nil {
						return fmt.Errorf("invalid request")
					}
					mode := strings.ToLower(strings.TrimSpace(update.Stack))
					if !validTunStack(mode) {
						return fmt.Errorf("unknown stack %q", update.Stack)
					}
					if onPick == nil {
						return nil
					}
					return onPick(mode)
				}, closeWindow)
		}, onClosed)
}

func validTunStack(mode string) bool {
	for _, stack := range singbox.TunStacks {
		if stack == mode {
			return true
		}
	}
	return false
}

// singboxStackOf is the stack a configuration is running on, defaulting to what
// every config written before the picker existed already carries.
func singboxStackOf(cfg *core.AppConfig) string {
	if cfg == nil || cfg.Singbox.Stack == "" {
		return singbox.DefaultTunStack
	}
	return cfg.Singbox.Stack
}
