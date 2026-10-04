//go:build darwin

package vpnmgr

import (
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"jorbiquevpn/internal/macos/applekit"
	"jorbiquevpn/internal/macos/core"
)

// DaemonLoop runs the event-driven daemon: an SCDynamicStore network watcher,
// an IOKit sleep/wake watcher and a CFRunLoop, via applekit's C-ABI bindings
// (no cgo). The event handlers are in daemon_handlers.go. Watcher install is
// best-effort: if either fails the daemon still runs its boot autostart and the
// configured SSID-poll/heartbeat tickers.
func (m *Manager) DaemonLoop() error {
	// The CFRunLoop must run on a fixed OS thread; pin it.
	runtime.LockOSThread()

	m.Logger.Log("Daemon started (event-driven, purego).")
	// Boot autostart is a choice, and the daemon is now installed whether or not
	// it was made: without the marker this process serves the control socket and
	// leaves the tunnels alone, which is what a machine that never asked for
	// autostart did when the daemon was not installed at all.
	if core.BootAutostartEnabled(m.Paths) {
		if err := m.AutostartCheck(false, false); err != nil {
			m.Logger.Error("autostartCheck failed: " + err.Error())
		}
	} else {
		m.Logger.Log("boot autostart is off; serving privileged requests only")
	}
	if ssid := m.NetworkSetup.GetCurrentSSID(); ssid != "" {
		m.saveLastSSID(ssid)
	}
	if ip := m.NetworkSetup.GetCurrentIP(); ip != "" {
		m.saveLastIP(ip)
	}

	// The watchers stay installed whatever the autostart setting is: the
	// handlers ask on every event (Manager.healingWanted), so turning it on
	// takes effect without restarting this process — and restarting this
	// process is exactly what cannot be done from a request it is serving.
	watching := false
	if err := applekit.InstallNetworkWatcher(m.handleNetworkChange); err != nil {
		m.Logger.Warn("SCDynamicStore watcher install failed: " + err.Error())
	} else {
		watching = true
	}
	if err := applekit.InstallPowerWatcher(m.handlePowerWillSleep, m.handlePowerHasPoweredOn); err != nil {
		m.Logger.Warn("IOKit power watcher install failed: " + err.Error())
	} else {
		watching = true
	}

	if srv := m.startControlServer(); srv != nil {
		defer srv.Close()
	}
	m.startDaemonTickers()

	// Exit cleanly when launchd (SIGTERM) or the user (SIGINT) asks us to stop,
	// instead of blocking forever and relying on a hard kill.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)

	m.Logger.Log("Daemon watchers installed; entering RunLoop.")
	if watching {
		go func() {
			<-stop
			m.Logger.Log("Daemon received shutdown signal; stopping run loop.")
			applekit.StopCFRunLoop()
		}()
		applekit.RunCFRunLoop() // blocks until StopCFRunLoop fires on a signal
	} else {
		// No watchers installed; the tickers (if any) keep reacting. Block until a
		// shutdown signal so the LaunchDaemon job stays alive until then.
		<-stop
		m.Logger.Log("Daemon received shutdown signal; exiting.")
	}
	return nil
}
