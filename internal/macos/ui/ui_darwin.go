//go:build darwin

package ui

import (
	"os"
	"path/filepath"
	"time"

	"jorbiquevpn/internal/constants"
	"jorbiquevpn/internal/logging"
	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/macos/launchd"
	"jorbiquevpn/internal/macos/procutil"
)

// barHandoffMarker, in the run directory, tells the agent's bar that a manual
// launch started it just now, not the login.
const barHandoffMarker = "bar.handoff"

// RunStatusBarApp launches the menu-bar application using the pure-Go/purego UI.
// It is the sole macOS entry point (the Objective-C/cgo path was removed in the
// cutover): it performs the LaunchAgent/single-instance bootstrap, then hands off
// to RunStatusBarAppPurego. Must be called from the main goroutine.
func RunStatusBarApp() {
	tempPaths := core.NewPaths()
	cfg, _ := core.ReadConfig(tempPaths)
	paths := tempPaths
	if cfg != nil {
		ep := cfg.EffectivePaths()
		paths = core.NewPathsWithEffective(&ep)
	}
	exePath := paths.ExecutablePath()
	handoffMarker := filepath.Join(paths.RunDir, barHandoffMarker)
	fromAgent := os.Getenv("JORBIQUE_VPN_LAUNCH_AGENT") == "1"

	// LaunchAgent starts before the menu bar is ready at login; a short delay
	// avoids an invisible icon. A bar handed over from a manual launch (below)
	// starts into a running session and skips it.
	if fromAgent {
		if os.Remove(handoffMarker) != nil {
			time.Sleep(2 * time.Second)
		}
		_ = launchd.RefreshAppLaunchAgentIfInstalled(exePath, "")
	}

	// Single-instance: prevent duplicate icons after a long sleep.
	pidFile := filepath.Join(paths.RunDir, "bar.pid")
	if pid, ok := procutil.ReadPID(pidFile); ok && procutil.IsAlive(pid) {
		if isBarCommandLine(procutil.CommandLine(pid), filepath.Base(exePath)) {
			ForwardLaunchLink()
			os.Exit(0)
		}
		_ = os.Remove(pidFile)
	}

	// The bar the user opens stays up until they quit it: hand it to the
	// LaunchAgent, whose KeepAlive restarts it when macOS ends the process
	// around sleep or a closed lid. The tunnel is another matter — it comes
	// back after sleep only with «Включать VPN при загрузке». Skipped when the
	// agent itself launched us; when launchd refuses, this copy runs the bar.
	if !fromAgent {
		_ = os.MkdirAll(paths.RunDir, 0o755)
		_ = os.WriteFile(handoffMarker, nil, 0o644)
		if launchd.HandOffToAppLaunchAgent(exePath, "") {
			// The agent now owns the bar; a link this copy was opened with goes to it.
			ForwardLaunchLink()
			return
		}
		_ = os.Remove(handoffMarker)
	}

	procutil.WritePID(os.Getpid(), pidFile)
	defer func() { _ = os.Remove(pidFile) }()

	// The menu bar's own logs, in the user's Library; root's are the daemon's.
	if paths.LogDir != paths.SystemLogDir {
		logging.CapLogsEvery(paths.LogDir)
	}

	if err := RunStatusBarAppPurego(exePath, constants.AppDisplayName); err != nil {
		p := core.NewPaths()
		_ = os.MkdirAll(p.LogDir, 0o755)
		line := time.Now().Format(time.RFC3339) + " menu bar failed: " + err.Error() + "\n"
		if f, e := os.OpenFile(filepath.Join(p.LogDir, "app.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); e == nil {
			_, _ = f.WriteString(line)
			_ = f.Close()
		}
		os.Exit(core.ExitGeneral)
	}
}
