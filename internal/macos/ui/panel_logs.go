//go:build darwin

package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"jorbiquevpn/internal/localapi"
	"jorbiquevpn/internal/macos/ansiutil"
	"jorbiquevpn/internal/macos/applekit"
	"jorbiquevpn/internal/macos/core"
)

// logsState is what the Logs window opens with: the logs it can show.
type logsState struct {
	Files []string `json:"files"`
}

// showLogsPanel opens the same log viewer the Windows client has. clear
// empties every log the list shows.
func showLogsPanel(paths *core.Paths, clear func() error, onClosed func()) (*webPanel, error) {
	assets, err := frontendAssets()
	if err != nil {
		return nil, err
	}
	return showWebPanel("Логи", 900, 640, 560, 420,
		func(closeWindow func()) (*localapi.LocalServer, error) {
			return localapi.StartFormServerFunc(panelQuery("logs"), "logs", assets,
				func() any { return logsState{Files: logFileNames(paths)} },
				func(json.RawMessage) error { return nil },
				map[string]func(json.RawMessage) (any, error){
					"log-content": func(body json.RawMessage) (any, error) {
						var request struct {
							Name string `json:"name"`
						}
						if err := json.Unmarshal(body, &request); err != nil {
							return nil, fmt.Errorf("invalid request")
						}
						return readLog(paths, request.Name)
					},
					"clear-logs": func(json.RawMessage) (any, error) {
						return struct{}{}, clear()
					},
				},
				closeWindow)
		}, onClosed)
}

// cmdOpenLogs opens the Logs window, or brings it forward when it is open.
func (c *statusBarController) cmdOpenLogs() {
	if c.logsP != nil {
		c.logsP.Show()
		return
	}
	_, paths := c.loadConfig()
	panel, err := showLogsPanel(paths,
		func() error { return c.clearLogs(paths) },
		func() { c.logsP = nil })
	if err != nil {
		applekit.PresentAlert("Не удалось открыть логи", err.Error(), applekit.AlertWarning)
		return
	}
	c.logsP = panel
}

// clearLogs empties the menu bar's own logs here and root's through the daemon.
func (c *statusBarController) clearLogs(paths *core.Paths) error {
	if paths.LogDir != paths.SystemLogDir {
		for _, name := range logNamesIn(paths.LogDir, "") {
			if err := os.Truncate(filepath.Join(paths.LogDir, name), 0); err != nil {
				return fmt.Errorf("clear %s: %w", name, err)
			}
		}
	}
	if out, status := c.run.runAndCaptureOutErr(c.exePath, []string{"clear-logs"}); status != 0 {
		return fmt.Errorf("%s", strings.TrimSpace(ansiutil.Strip(out)))
	}
	return nil
}
