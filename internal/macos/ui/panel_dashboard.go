//go:build darwin

package ui

import (
	"path/filepath"

	"jorbiquevpn/internal/constants"
	"jorbiquevpn/internal/dashboard"
	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/macos/procutil"
)

func showDashboardPanel(configPath string, memoryFallback func() uint64, onClosed func()) (*webPanel, error) {
	assets, err := frontendAssets()
	if err != nil {
		return nil, err
	}
	return showWebPanel(constants.AppDisplayName+" Dashboard", 1040, 720, 720, 540, func(func()) (*dashboard.LocalServer, error) {
		// No Cancel button on this page: the dashboard is closed with the window.
		return dashboard.StartLocalServer(panelQuery("dashboard"), configPath, assets, memoryFallback)
	}, onClosed)
}

func singboxResidentMemory(cfg *core.AppConfig, runDir string) uint64 {
	if cfg == nil {
		return 0
	}
	for _, vpn := range cfg.VPNs {
		if vpn.Type == core.VPNSingbox {
			pid, ok := procutil.ReadPID(filepath.Join(runDir, vpn.Name+".pid"))
			if ok {
				return procutil.ResidentMemory(pid)
			}
		}
	}
	return 0
}
