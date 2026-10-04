package vpnmgr

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"jorbiquevpn/internal/constants"
	"jorbiquevpn/internal/macos/ansiutil"
	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/macos/procutil"
	"jorbiquevpn/internal/macos/routerules"
	"jorbiquevpn/internal/macos/singbox"
)

func (m *Manager) resolveTargets(name string) []core.VPNItem {
	var candidates []core.VPNItem
	if name == "all" {
		candidates = m.Cfg.VPNs
	} else {
		for _, v := range m.Cfg.VPNs {
			if v.Name == name {
				candidates = append(candidates, v)
			}
		}
	}
	return candidates
}

// requireUserUUID keeps every tunnel down until the first-run window has
// stored a UUID the server knows.
// The menu bar only shows that window; this is what holds for the daemon's
// autostart and healing and for `start` typed in a terminal.
func (m *Manager) requireUserUUID() error {
	if strings.TrimSpace(m.Cfg.Singbox.UUID) == "" {
		return core.BadConfig("No user UUID yet: open " + constants.AppDisplayName + " and enter it")
	}
	return nil
}

func (m *Manager) startTargets(targets []core.VPNItem) error {
	if err := m.requireUserUUID(); err != nil {
		return err
	}
	// Nothing here decides anything — the start goes ahead either way — so a
	// Manager without NetworkSetup (the shape tests build) simply skips it.
	if m.NetworkSetup != nil {
		m.Logger.Debug("Checking network connection...")
		if !m.NetworkSetup.WaitForNetworkConnection(context.Background(), 60) {
			m.Logger.Warn("Network not connected, but continuing anyway")
		} else {
			m.Logger.Debug("Network connected")
		}
	}

	started, skipped, failures := m.startBatch(targets)

	listSet := map[string]struct{}{}
	for _, n := range m.loadStartedList() {
		listSet[n] = struct{}{}
	}
	// Only what actually came up. The list is what an autostart restores, and a
	// VPN that failed to start is not something to bring back at the next boot.
	for _, v := range started {
		listSet[v.Name] = struct{}{}
	}
	// A VPN another process is already starting belongs on the list too. It is
	// wanted up — that is what asking to start it means — and the
	// sing-box update reaches here through Stop-then-Start, which has already
	// taken the name off the list on disk. Leaving it out because this call lost
	// a lock race would drop a running tunnel out of the autostart set until
	// someone started it by hand again.
	for _, v := range skipped {
		listSet[v.Name] = struct{}{}
	}
	names := make([]string, 0, len(listSet))
	for k := range listSet {
		names = append(names, k)
	}
	m.saveStartedList(names)

	m.flushDNS()
	m.performPostStartConnectivityChecks(started)
	if len(failures) > 0 {
		// In the log as well as in the answer: a start that refused before it
		// ran anything left nothing behind but the DNS being restored, and the
		// reason lived only in a reply nobody wrote down.
		m.Logger.Error("start failed: " + strings.Join(failures, "; "))
		return core.StartFailed(strings.Join(failures, "; "))
	}
	return nil
}

// startBatch starts the configured sing-box cores and reports every outcome.
func (m *Manager) startBatch(ordered []core.VPNItem) (started, skipped []core.VPNItem, failures []string) {
	return startBatchWith(ordered, m.startOne)
}

func startBatchWith(ordered []core.VPNItem, start func(core.VPNItem) error) (started, skipped []core.VPNItem, failures []string) {
	for _, v := range ordered {
		switch err := start(v); {
		case errors.Is(err, errStartInProgress):
			skipped = append(skipped, v)
		case err != nil:
			failures = append(failures, v.Name+": "+err.Error())
		default:
			started = append(started, v)
		}
	}
	sort.Slice(started, func(i, j int) bool { return started[i].Name < started[j].Name })
	sort.Slice(skipped, func(i, j int) bool { return skipped[i].Name < skipped[j].Name })
	sort.Strings(failures)
	return started, skipped, failures
}

// Start starts one or more VPNs by name (or "all").
func (m *Manager) Start(names []string) error {
	var allTargets []core.VPNItem
	seen := map[string]struct{}{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		ts := m.resolveTargets(n)
		if len(ts) == 0 && n != "all" {
			return core.BadConfig("Unknown vpn name: " + n)
		}
		for _, t := range ts {
			if _, ok := seen[t.Name]; !ok {
				seen[t.Name] = struct{}{}
				allTargets = append(allTargets, t)
			}
		}
	}
	if len(allTargets) == 0 {
		return nil
	}
	return m.startTargets(allTargets)
}

// Stop stops a VPN by name or "all".
func (m *Manager) Stop(name string) {
	if name == "all" {
		m.saveStartedList(nil)
		time.Sleep(time.Second)
		m.stopAllInternal()
		return
	}
	// By name alone, not resolveTargets: stopping reads no profile, and a VPN
	// whose file has gone would be skipped there and left running.
	var targets []core.VPNItem
	for _, v := range m.Cfg.VPNs {
		if v.Name == name {
			targets = append(targets, v)
		}
	}
	// Drop the targets from the started list before stopping them, the way the
	// "all" path above does: stopOne clears the pid files up front, and until
	// the list is rewritten the daemon would see a VPN that is "not running"
	// but still wanted, and start it right back up.
	list := m.loadStartedList()
	rm := map[string]struct{}{}
	for _, t := range targets {
		rm[strings.ToLower(t.Name)] = struct{}{}
	}
	out := []string{}
	for _, n := range list {
		if _, ok := rm[strings.ToLower(n)]; !ok {
			out = append(out, n)
		}
	}
	m.saveStartedList(out)
	for _, v := range targets {
		m.stopOne(v)
	}
}

// Restart restarts one or more VPNs by name, mirroring Start/Stop: "all" means
// every currently running VPN. A named VPN that is not running is simply
// started — the same stop-if-running + start the autostart recheck performs.
func (m *Manager) Restart(names []string) error {
	var targets []core.VPNItem
	seen := map[string]struct{}{}
	add := func(items []core.VPNItem) {
		for _, t := range items {
			key := strings.ToLower(t.Name)
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				targets = append(targets, t)
			}
		}
	}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if n == "all" {
			add(m.runningVPNs())
			continue
		}
		ts := m.resolveTargets(n)
		if len(ts) == 0 {
			return core.BadConfig("Unknown vpn name: " + n)
		}
		add(ts)
	}
	if len(targets) == 0 {
		fmt.Println("No running VPNs to restart.")
		return nil
	}
	return m.restartTargets(targets)
}

// restartTargets stops (when running) and starts each target,
// then reapplies adapter DNS — the same sequence the autostart recheck uses for
// its force restart. The started list is left untouched: restarting does not
// change which VPNs autostart owns.
func (m *Manager) restartTargets(targets []core.VPNItem) error {
	if err := m.requireUserUUID(); err != nil {
		return err
	}
	_, failures := m.restartBatch(targets, 2)
	if len(failures) > 0 {
		return core.StartFailed(strings.Join(failures, "; "))
	}
	return nil
}

// restartBatch stops all targets before restarting them and reports failures.
func (m *Manager) restartBatch(ordered []core.VPNItem, attempts int) (started []core.VPNItem, failures []string) {
	for _, v := range ordered {
		if m.IsRunning(v) {
			m.stopOne(v)
		}
	}
	started, _, failures = startBatchWith(ordered, func(v core.VPNItem) error { return m.startOneWithRetry(v, attempts) })
	return started, failures
}

func (m *Manager) stopAllInternal() {
	for _, v := range m.Cfg.VPNs {
		m.stopOne(v)
	}
}

// flushDNS drops the system's cached answers: they were resolved on the other
// side of a connect or a stop, and some of them are wrong on this one. Only a
// Manager that was given a NetworkSetup touches the machine — the shape tests
// build has none.
func (m *Manager) flushDNS() {
	if m.NetworkSetup == nil {
		return
	}
	for _, cmd := range [][]string{{"/usr/bin/dscacheutil", "-flushcache"}, {"/usr/bin/killall", "-HUP", "mDNSResponder"}} {
		if st, _ := procutil.RunProcess(cmd[0], cmd[1:], true); st != 0 {
			m.Logger.Debug(fmt.Sprintf("%s exit=%d", strings.Join(cmd, " "), st))
		}
	}
}

func (m *Manager) startOneWithRetry(v core.VPNItem, maxAttempts int) error {
	if maxAttempts == 0 {
		maxAttempts = m.Cfg.Connectivity.MaxAttempts
	}
	baseDelay := m.Cfg.Connectivity.RetryDelay
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := m.startOne(v); err == nil {
			return nil
		} else if errors.Is(err, errStartInProgress) {
			// Retrying would only lose the same race again, slower.
			return err
		} else {
			lastErr = err
			m.Logger.Error(fmt.Sprintf("autostart-check: failed to restart %s (attempt %d/%d): %s", v.Name, attempt, maxAttempts, err.Error()))
			if attempt < maxAttempts {
				delay := baseDelay << (attempt - 1)
				if delay > 30 {
					delay = 30
				}
				m.Logger.Debug(fmt.Sprintf("autostart-check: retrying %s in %ds (exponential backoff)...", v.Name, delay))
				time.Sleep(time.Duration(delay) * time.Second)
			}
		}
	}
	return lastErr
}

func (m *Manager) startOne(v core.VPNItem) error {
	if v.Type != core.VPNSingbox {
		return core.BadConfig("unsupported VPN type " + string(v.Type) + "; use the sing-box core")
	}
	// Held for the whole start, so the "already running?" test below and the
	// spawn that follows it cannot be split by another process doing the same.
	release, ok := m.lockStart(v.Name)
	if !ok {
		m.Logger.Log("start: " + v.Name + " is already being started by another process; leaving it to that one")
		return errStartInProgress
	}
	defer release()
	if m.IsRunning(v) {
		return nil
	}
	pidPath := m.Paths.PIDFile(v.Name)
	logPath := m.Paths.LogFile(v.Name)

	if v.File == "" {
		return core.BadConfig("singbox '" + v.Name + "' has empty file")
	}
	// No existence check: this path writes that file from the downloaded
	// profile a few lines below, and refusing to start without it meant a
	// deleted config could only be restored by hand.
	sbPath := m.Paths.ResolveFromCfg(v.File)
	if err := os.MkdirAll(filepath.Dir(sbPath), 0o755); err != nil {
		return core.StartFailed("cannot create the config directory for " + v.Name + ": " + err.Error())
	}
	m.Logger.Debug("Using sing-box config: " + sbPath)
	if m.Cfg.Singbox.NaiveMode {
		if err := m.ensureNaiveSingbox(); err != nil {
			return err
		}
	}

	uuid := strings.TrimSpace(m.Cfg.Singbox.UUID)
	enabled := m.effectiveEnabledSingboxOutbounds()
	disabled := m.disabledSingboxOutbounds()
	baseConfigPath := filepath.Join(filepath.Dir(sbPath), "singbox.json")
	root, err := singbox.BuildConfigFromRemoteTemplate(core.SingboxSection{UUID: uuid, EnabledOutbounds: enabled, DisabledOutbounds: disabled, SelectedNode: m.selectedNode(), NaiveMode: m.Cfg.Singbox.NaiveMode}, baseConfigPath, 12*time.Second, m.Logger.Warn)
	if err != nil {
		return err
	}
	// Persist sing-box's DNS cache across stop/start: without cache_file the
	// resolver cache lives only in process memory and dies with every stop.
	// A directory and a cache_id per profile, so two sing-box profiles never
	// share one cache.db. Absolute path because sing-box resolves relative ones
	// against its cwd, which differs between daemon and CLI starts. The file
	// outlives stops on purpose; only `clear-singbox-cache` (menu-bar Quit)
	// removes it.
	cacheID := singboxCacheID(v.Name)
	cacheDir := filepath.Join(m.singboxCacheRoot(), cacheID)
	_ = os.MkdirAll(cacheDir, 0o755)
	cacheFile := singbox.NewOrderedMap()
	cacheFile.Set("enabled", true)
	cacheFile.Set("path", filepath.Join(cacheDir, "cache.db"))
	cacheFile.Set("cache_id", cacheID)
	cacheFile.Set("store_fakeip", false)
	cacheFile.Set("store_dns", true)
	experimental := singbox.NewOrderedMap()
	experimental.Set("cache_file", cacheFile)
	root.Set("experimental", experimental)
	singbox.NormalizeLogOutput(root, m.Paths.LogDir)
	if err := singbox.WriteJSONFileAtomically(root, sbPath); err != nil {
		return err
	}
	m.Logger.Log(ansiutil.Green("✓ Updated sing-box config from remote template at " + sbPath))
	if err := routerules.ReapplySavedRouteRulesToSingboxFiles(m.Paths, m.Cfg); err != nil {
		m.Logger.Warn("Could not re-apply route rules after sing-box template: " + err.Error())
	} else {
		m.Logger.Log(ansiutil.Green("✓ Re-applied route rules / DNS to sing-box after remote template"))
	}
	// Last word on the file's shape belongs to the binary that reads it.
	m.normalizeSingboxConfigs()

	args := m.singboxCommand(v, sbPath)
	// The program is the first argument and it is an absolute path, so the
	// /usr/bin/env hop this used to take resolved nothing and only added a
	// process between us and the tunnel.
	spawnPid, err := procutil.Spawn(args[0], args[1:], logPath, pidPath, true)
	if err != nil {
		return err
	}
	time.Sleep(time.Second)
	if !procutil.IsAlive(spawnPid) {
		fmt.Println(ansiutil.Red("sing-box process exited. Last 15 lines of log:"))
		fmt.Println("---")
		fmt.Println(procutil.LastLines(logPath, 15))
		fmt.Println("---")
		return core.StartFailed("sing-box did not stay running for " + v.Name)
	}
	procutil.WritePID(spawnPid, pidPath)
	m.Logger.Debug(fmt.Sprintf("Started %s (pid=%d)", v.Name, spawnPid))
	fmt.Println(ansiutil.Green("✓ singbox started"))
	return nil
}

func (m *Manager) singboxCommand(v core.VPNItem, sbPath string) []string {
	sbCandidate := "sing-box"
	if v.Exec != nil {
		sbCandidate = *v.Exec
	}
	sbExec := ""
	if strings.EqualFold(strings.TrimSpace(sbCandidate), "sing-box") && strings.TrimSpace(m.singboxExe) != "" {
		sbExec = m.singboxExe
	}
	if sbExec == "" {
		sbExec = procutil.Which(sbCandidate)
	}
	if sbExec == "" {
		sbExec = sbCandidate
	}
	var sbArgs []string
	if v.Args != nil {
		sbArgs = v.Args
	} else {
		sbArgs = []string{"run", "-c", sbPath}
	}
	return append([]string{sbExec}, sbArgs...)
}

func (m *Manager) stopOne(v core.VPNItem) {
	// Find the process, then drop the pid files *before* terminating it.
	// Everything that asks "is this VPN up" — `status`, the menu bar, the
	// daemon's checks — answers from those files, and Terminate waits out a
	// grace period. Leaving them in place meant a status refresh landing in that
	// window still counted the tunnel as running and fired an HTTP probe at a
	// host that was already on its way to unreachable: the probe then blocked
	// for its full timeout and reported FAIL seconds after the user had stopped
	// the VPN. Nothing probes a tunnel that is going away now.
	pid, alive := 0, false
	for _, path := range m.pidFileCandidatesForVPN(v.Name) {
		if p, ok := procutil.ReadPID(path); ok && procutil.IsAlive(p) {
			pid, alive = p, true
			m.Logger.Debug(fmt.Sprintf("Stopping %s pid=%d (pidfile %s)", v.Name, pid, path))
			break
		}
	}
	for _, path := range m.pidFileCandidatesForVPN(v.Name) {
		_ = os.Remove(path)
	}
	if alive {
		procutil.Terminate(pid, 3*time.Second)
		fmt.Println(ansiutil.Yellow("Stopped " + v.Name))
	} else {
		fmt.Println(ansiutil.Yellow("Already stopped " + v.Name))
	}
	m.flushDNS()
}

// singboxCacheRoot is the directory holding one cache subdirectory per sing-box
// profile. Next to the binary, so it is root-owned like everything else the
// daemon writes there.
func (m *Manager) singboxCacheRoot() string {
	if strings.TrimSpace(m.Paths.CacheDir) != "" {
		return m.Paths.CacheDir
	}
	return filepath.Join(m.Paths.BinDir, "cache")
}

// singboxCacheID turns a VPN name into a cache_id / directory name: lowercase,
// no spaces or separators.
func singboxCacheID(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	id := strings.Trim(b.String(), "_")
	if id == "" {
		return "singbox"
	}
	return id
}

// ClearSingboxCache removes the persistent sing-box cache (its on-disk DNS
// cache). Called from the menu bar's Quit — full app shutdown — and never from
// stop, so the cache survives ordinary stop/start cycles. It refuses to act
// while sing-box is still up: a live tunnel would just write the cache back out
// at its own shutdown, and dropping the file under it buys nothing.
func (m *Manager) ClearSingboxCache() error {
	for _, v := range m.Cfg.VPNs {
		if v.Type == core.VPNSingbox && m.IsRunning(v) {
			m.Logger.Warn("sing-box cache kept: " + v.Name + " is still running")
			return nil
		}
	}
	dir := m.singboxCacheRoot()
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	m.Logger.Log("sing-box cache cleared (" + dir + ")")
	return nil
}
