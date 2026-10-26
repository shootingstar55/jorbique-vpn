//go:build darwin

package ui

import (
	"os"
	"path/filepath"
	"time"

	"jorbiquevpn/internal/constants"
	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/macos/launchd"
	"jorbiquevpn/internal/macos/procutil"
)

// RunStatusBarApp launches the menu-bar application using the pure-Go/purego UI.
// It is the sole macOS entry point (the Objective-C/cgo path was removed in the
// cutover): it performs the LaunchAgent/single-instance bootstrap, then hands off
// to RunStatusBarAppPurego. Must be called from the main goroutine.
func RunStatusBarApp() {
	// LaunchAgent starts before the menu bar is ready; a short delay avoids an
	// invisible icon.
	if os.Getenv("JORBIQUE_VPN_LAUNCH_AGENT") == "1" {
		time.Sleep(2 * time.Second)
	}

	tempPaths := core.NewPaths()
	cfg, _ := core.ReadConfig(tempPaths)
	paths := tempPaths
	if cfg != nil {
		ep := cfg.EffectivePaths()
		paths = core.NewPathsWithEffective(&ep)
	}
	exePath := paths.ExecutablePath()
	_ = launchd.RefreshAppLaunchAgentIfInstalled(exePath, "")

	// Re-apply a remembered app autostart. Quit boots the agent out (so it cannot
	// respawn) but keeps the plist, leaving autostart "installed, not loaded". When
	// the user relaunches by hand, hand the bar back to the agent: bootstrapping it
	// re-arms login autostart and makes the status read "installed & loaded" again,
	// and exiting avoids a manual instance competing with the agent's unconditional
	// KeepAlive in a respawn loop. Skipped when the agent itself launched us
	// (JORBIQUE_VPN_LAUNCH_AGENT=1) or when autostart is disabled (no plist).
	if os.Getenv("JORBIQUE_VPN_LAUNCH_AGENT") != "1" {
		if launchd.BootstrapAppLaunchAgentIfDormant("") {
			// The agent now owns the bar; a link this copy was opened with goes to it.
			ForwardLaunchLink()
			return
		}
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
	procutil.WritePID(os.Getpid(), pidFile)
	defer func() { _ = os.Remove(pidFile) }()

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
