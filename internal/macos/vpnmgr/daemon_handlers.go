package vpnmgr

// Pure-Go daemon event handlers. They react to network changes and
// sleep/wake; the watcher plumbing that calls them lives in daemon_purego.go.

import (
	"fmt"
	"time"

	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/telemetry"
)

// healingWanted reports whether this daemon should be bringing tunnels back.
//
// It is asked on every event rather than once at startup, so turning boot
// autostart on or off takes effect immediately. The alternative — reading it
// once and restarting the daemon when it changes — is what broke: the restart
// was performed by a process the daemon had started, so the daemon killed the
// child that was supposed to bring it back.
//
// With autostart off the menu bar does its own healing, and a second healer
// here would race it.
func (m *Manager) healingWanted() bool {
	return core.BootAutostartEnabled(m.Paths)
}

func (m *Manager) handleNetworkChange() {
	if !m.healingWanted() {
		return
	}
	curSSID := m.NetworkSetup.GetCurrentSSID()
	curIP := m.NetworkSetup.GetCurrentIP()
	lastSSID := m.loadLastSSID()
	lastIP := m.loadLastIP()
	reason := ""
	if curSSID != "" && lastSSID != curSSID {
		reason = fmt.Sprintf("SSID change (%s -> %s)", emptyTo(lastSSID, "none"), curSSID)
	} else if curSSID == "" && lastSSID != "" {
		m.saveLastSSID("")
	}
	if curIP != "" && lastIP != curIP {
		ipReason := fmt.Sprintf("IP change (%s -> %s)", emptyTo(lastIP, "none"), curIP)
		if reason != "" {
			reason += " + IP change"
		} else {
			reason = ipReason
		}
	}
	if reason != "" {
		m.Logger.Log(reason + " -> schedule autostart-check")
		if curSSID != "" {
			m.saveLastSSID(curSSID)
		}
		if curIP != "" {
			m.saveLastIP(curIP)
		}
		delay := m.Cfg.EffectiveAutostart().NetworkChangeDelaySeconds
		m.ScheduleAutostartCheck("Network change", delay, false, true)
	}
}

func emptyTo(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func (m *Manager) handlePowerWillSleep() {
	m.autoMu.Lock()
	m.isSystemSleeping = true
	m.autoMu.Unlock()
	m.Logger.Log("System will sleep -> pausing checks (not stopping VPNs)")
}

func (m *Manager) handlePowerHasPoweredOn() {
	if !m.healingWanted() {
		return
	}
	m.autoMu.Lock()
	m.isSystemSleeping = false
	pending := m.hasPendingAutoCheck
	m.hasPendingAutoCheck = false
	m.autoMu.Unlock()
	reason := "Wake from sleep"
	if pending {
		reason = "Wake from sleep (pending network changes)"
	}
	m.ScheduleAutostartCheck(reason, m.Cfg.EffectiveAutostart().WakeDelaySeconds, false, false)
}

func (m *Manager) checkSSIDChange() {
	if !m.healingWanted() {
		return
	}
	curSSID := m.NetworkSetup.GetCurrentSSID()
	lastSSID := m.loadLastSSID()
	if curSSID == lastSSID {
		return
	}
	if curSSID != "" {
		m.saveLastSSID(curSSID)
	} else if lastSSID != "" {
		m.saveLastSSID("")
	}
	reason := "SSID change (" + emptyTo(lastSSID, "none") + " -> " + emptyTo(curSSID, "none") + ")"
	m.Logger.Log(reason + " -> schedule autostart-check")
	m.ScheduleAutostartCheck(reason, m.Cfg.EffectiveAutostart().NetworkChangeDelaySeconds, false, true)
}

func (m *Manager) runHeartbeatCheck() {
	if !m.healingWanted() {
		return
	}
	if m.Cfg.EffectiveAutostart().IsInQuietHours(time.Now()) {
		return
	}
	m.ScheduleAutostartCheck("Heartbeat", time.Second, false, true)
}

// startDaemonTickers launches the optional SSID-poll and heartbeat tickers
// configured in autostart. Shared by both daemon implementations.
func (m *Manager) startDaemonTickers() {
	if interval := m.Cfg.EffectiveAutostart().SSIDPollIntervalSeconds; interval != nil && *interval > 0 {
		dt := time.Duration(*interval) * time.Second
		go func() {
			t := time.NewTicker(dt)
			defer t.Stop()
			for range t.C {
				m.checkSSIDChange()
			}
		}()
	}
	if interval := m.Cfg.EffectiveAutostart().HeartbeatIntervalSeconds; interval != nil && *interval > 0 {
		dt := time.Duration(*interval) * time.Second
		go func() {
			t := time.NewTicker(dt)
			defer t.Stop()
			for range t.C {
				m.runHeartbeatCheck()
			}
		}()
	}
	// Read from disk on every push, not m.Cfg: the menu bar saves the UUID
	// after the daemon has started.
	telemetry.Start(m.Paths.StateDir(), func() string {
		cfg, err := core.ReadConfig(m.Paths)
		if err != nil {
			return ""
		}
		return cfg.Singbox.UUID
	}, m.tunnelStates, m.localSingboxVersion, m.Logger.Log)
}
