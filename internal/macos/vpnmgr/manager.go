package vpnmgr

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"jorbiquevpn/internal/autoconnect"
	"jorbiquevpn/internal/macos/ansiutil"
	"jorbiquevpn/internal/macos/control"
	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/macos/directbypass"
	"jorbiquevpn/internal/macos/ipgeo"
	"jorbiquevpn/internal/macos/launchd"
	"jorbiquevpn/internal/macos/logger"
	"jorbiquevpn/internal/macos/netsetup"
	"jorbiquevpn/internal/macos/procutil"
	"jorbiquevpn/internal/macos/routerules"
	"jorbiquevpn/internal/macos/singbox"
	"jorbiquevpn/internal/security"
	"jorbiquevpn/internal/telemetry"
	"jorbiquevpn/internal/webprobe"
)

const (
	systemDomainPrefix  = "system/"
	launchctlExecutable = "/bin/launchctl"
	singboxBinaryName   = "sing-box"
)

// Manager is the central VPN orchestrator.
type Manager struct {
	Paths        *core.Paths
	Cfg          *core.AppConfig
	Logger       *logger.Logger
	NetworkSetup *netsetup.NetworkSetup

	// singboxExe is a resolved path to sing-box (bundled next to the binary or from PATH).
	singboxExe      string
	connectionProbe autoconnect.Probe

	// Modification time of the settings the daemon last read.
	cfgModTime time.Time

	autoMu               sync.Mutex
	pendingAutoCancel    chan struct{}
	isAutoCheckRunning   bool
	isSystemSleeping     bool
	hasPendingAutoCheck  bool
	lastAutostartTime    time.Time
	hasLastAutostartTime bool
}

// New creates a Manager from the on-disk config.
func New() (*Manager, error) {
	tempPaths := core.NewPaths()
	tempLogger := logger.New(filepath.Join(tempPaths.LogDir, "app.log"))
	// A binary that moved (a reinstall to another prefix, a developer build)
	// leaves launchd starting the one that used to be there. The daemon is
	// root's to rewrite.
	if os.Geteuid() == 0 {
		if err := launchd.RefreshDaemonIfStale(tempPaths.ExecutablePath(), ""); err != nil {
			tempLogger.Warn("launchd: could not point the daemon at this binary: " + err.Error())
		}
	}
	cfg, err := core.ReadConfig(tempPaths)
	if err != nil {
		return nil, err
	}
	ep := cfg.EffectivePaths()
	paths := core.NewPathsWithEffective(&ep)

	wifiSvc := ""
	if cfg.Defaults != nil {
		wifiSvc = cfg.Defaults.WiFiService
	}
	// Prefer sing-box installed next to the binary, otherwise fall back to
	// PATH/Homebrew locations.
	singboxPath := bundledSingboxExecutable(paths.BinDir, tempLogger)
	warnMissingTools(cfg.RequiredTools, singboxPath, tempLogger)

	return &Manager{
		Paths:        paths,
		Cfg:          cfg,
		Logger:       tempLogger,
		NetworkSetup: netsetup.New(wifiSvc),
		singboxExe:   singboxPath,
	}, nil
}

func warnMissingTools(required []string, singboxPath string, log *logger.Logger) {
	for _, tool := range required {
		if tool == singboxBinaryName {
			if singboxPath == "" {
				log.Warn("tool not found: sing-box (run update-singbox, or put sing-box next to the client binary)")
			}
			continue
		}
		if procutil.Which(tool) == "" {
			log.Warn("tool not found: " + tool)
		}
	}
}

func bundledSingboxExecutable(binDir string, log *logger.Logger) string {
	singboxPath := ""
	{
		candidate := filepath.Join(binDir, singboxBinaryName)
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() && st.Mode().IsRegular() {
			if st.Mode().Perm()&0o111 == 0 {
				if cerr := os.Chmod(candidate, 0o755); cerr != nil {
					log.Warn("sing-box next to binary is not executable and chmod failed: " + cerr.Error())
				} else {
					st, err = os.Stat(candidate)
				}
			}
			if err == nil && st.Mode().Perm()&0o111 != 0 {
				singboxPath = candidate
			} else if err == nil {
				log.Warn("sing-box next to binary is still not executable: chmod +x " + candidate)
			}
		}
	}
	if singboxPath == "" {
		singboxPath = procutil.Which(singboxBinaryName)
	}

	return singboxPath
}

// effectiveEnabledSingboxOutbounds is what the configuration asks for, with the
// blanks removed. Which of those the server still offers, and in what order to
// try them, is decided against the downloaded profile — the only place that
// knows — in singbox.BuildConfigFromRemoteTemplate. An empty result means "no
// opinion", and everything the profile offers is used.
func (m *Manager) effectiveEnabledSingboxOutbounds() []string {
	out := []string{}
	for _, t := range m.Cfg.Singbox.EnabledOutbounds {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func (m *Manager) singboxConfigPathsToUpdate() []string {
	var out []string
	for _, v := range m.Cfg.VPNs {
		if v.Type != core.VPNSingbox {
			continue
		}
		p := m.Paths.ResolveFromCfg(v.File)
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	fb := filepath.Join(m.Paths.CfgDir, m.Paths.SingboxFallbackFile)
	if _, err := os.Stat(fb); err == nil {
		dup := false
		for _, p := range out {
			if p == fb {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, fb)
		}
	}
	return out
}

// runDirCandidates returns run directories that may contain PID files for this install.
// The menu bar runs as the user and the daemon as root, each with its own run
// directory — both must be visible for status/stop.
func (m *Manager) runDirCandidates() []string {
	primary := filepath.Clean(m.Paths.RunDir)
	out := []string{primary}
	seen := map[string]struct{}{primary: {}}
	add := func(s string) {
		s = filepath.Clean(strings.TrimSpace(s))
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	add(core.InstalledRunDir)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		add(filepath.Join(home, m.Cfg.EffectivePaths().RunDirUser, "run"))
		// Where an installed menu bar keeps its runtime state (core.newPathsAt).
		add(filepath.Join(home, "Library/Application Support/JorbiqueVPN/run"))
	}
	if ep := strings.TrimSpace(m.Cfg.EffectivePaths().RunDir); ep != "" {
		add(ep)
	}
	return out
}

// pidFileCandidatesForVPN lists PID file paths that may exist for this VPN (primary + common alternates).
func (m *Manager) pidFileCandidatesForVPN(name string) []string {
	var out []string
	seen := map[string]struct{}{}
	add := func(p string) {
		if p == "" {
			return
		}
		p = filepath.Clean(p)
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	add(m.Paths.PIDFile(name))
	for _, rd := range m.runDirCandidates() {
		add(filepath.Join(rd, name+".pid"))
	}
	return out
}

// runningVPNPID returns the live PID for v if any candidate pidfile points to a running process.
func (m *Manager) runningVPNPID(v core.VPNItem) (pid int, ok bool) {
	for _, p := range m.pidFileCandidatesForVPN(v.Name) {
		if pi, o := procutil.ReadPID(p); o && procutil.IsAlive(pi) {
			return pi, true
		}
	}
	return 0, false
}

// IsRunning returns true if the VPN's pid is alive.
func (m *Manager) IsRunning(v core.VPNItem) bool {
	_, ok := m.runningVPNPID(v)
	return ok
}

// reloadConfigIfChanged picks up what the menu bar saved since the daemon read
// vpn_cfg.json. The daemon lives for days, the control requests that change the
// file run in children of it, and a restart built from the config it started
// with asked for proxy outbounds the user had since turned off — failing with
// "sing-box uuid is empty" after it had already stopped the core.
//
// ponytail: unsynchronised swap of m.Cfg, fine for a pointer the other readers
// only dereference; atomic.Pointer if one ever holds it across a restart.
func (m *Manager) reloadConfigIfChanged() {
	st, err := os.Stat(m.Paths.CfgFile)
	if err != nil {
		return
	}
	if st.ModTime().Equal(m.cfgModTime) {
		return
	}
	cfg, err := core.ReadConfig(m.Paths)
	if err != nil {
		m.Logger.Warn("autostart-check: keeping the config already loaded: " + err.Error())
		return
	}
	m.Cfg, m.cfgModTime = cfg, st.ModTime()
}

// AutostartCheck implements the autostart logic.
func (m *Manager) AutostartCheck(forceRestart, fullStopAllFirst bool) error {
	m.autoMu.Lock()
	if m.isAutoCheckRunning {
		m.autoMu.Unlock()
		return nil
	}
	m.isAutoCheckRunning = true
	m.autoMu.Unlock()
	defer func() {
		m.autoMu.Lock()
		m.isAutoCheckRunning = false
		m.autoMu.Unlock()
	}()
	m.reloadConfigIfChanged()
	if m.requireUserUUID() != nil {
		m.Logger.Debug("autostart-check: no user UUID yet, nothing to bring up")
		return nil
	}

	ast := m.Cfg.EffectiveAutostart()
	m.Logger.Debug("autostart-check: checking network connection...")
	// No NetworkSetup means a Manager built by a test. Waiting on a network it
	// cannot ask about would stop every one of those before it reached the
	// branch it is testing.
	connected := m.NetworkSetup == nil
	if !connected {
		netCtx, netCancel := m.networkWaitContext()
		connected = m.NetworkSetup.WaitForNetworkConnection(netCtx, ast.WiFiWaitTimeoutSeconds)
		netCancel()
	}
	if !connected {
		m.Logger.Log("autostart-check: network not connected, skipping")
		return nil
	}
	m.Logger.Debug("autostart-check: network connected")

	targets := m.autostartTargets(ast.ExcludeNames)
	if len(targets) == 0 {
		return nil
	}
	if fullStopAllFirst {
		return m.restartAfterStopAll(targets)
	}

	if forceRestart {
		m.Logger.Log("autostart-check: force restart -> restarting saved targets only")
		_, failures := m.restartBatch(targets, 2)
		if len(failures) > 0 {
			m.Logger.Error("autostart-check: did not come back after the force restart: " + strings.Join(failures, "; "))
		}
		return nil
	}

	failing := m.stoppedAutostartTargets(targets)
	if len(failing) == 0 {
		m.Logger.Log("autostart-check: all targets running; no restart")
		return nil
	}
	restarted, failures := m.restartBatch(failing, 2)
	// Named from what came back, not from what was attempted. Logging the whole
	// set as "restarted" and the failures underneath it put two lines that
	// contradict each other next to each other in the log people read when a
	// tunnel did not survive the night.
	m.logAutostartResult(restarted, failures)
	return nil
}

func (m *Manager) autostartTargets(exclude map[string]struct{}) []core.VPNItem {
	listKeys := map[string]struct{}{}
	for _, n := range m.loadStartedList() {
		listKeys[strings.ToLower(n)] = struct{}{}
	}

	var targetsFromList, runningNow []core.VPNItem
	for _, v := range m.Cfg.VPNs {
		if _, ex := exclude[v.Name]; ex {
			continue
		}
		if _, ok := listKeys[strings.ToLower(v.Name)]; ok {
			targetsFromList = append(targetsFromList, v)
		}
		if m.IsRunning(v) {
			runningNow = append(runningNow, v)
		}
	}

	targets := targetsFromList
	if len(targets) == 0 {
		targets = runningNow
	}
	if len(targets) == 0 {
		m.logMissingAutostartTargets()
		return nil
	}

	if len(targetsFromList) == 0 && len(runningNow) > 0 {
		m.Logger.Warn("autostart-check: started-list empty/mismatched; using currently running targets: " + strings.Join(vpnItemNames(runningNow), ", "))
	}

	return targets
}

func (m *Manager) logMissingAutostartTargets() {
	url := m.startedListURL()
	_, err := os.Stat(url)
	exists := err == nil
	var size int64
	if st, err := os.Stat(url); err == nil {
		size = st.Size()
	}
	reason := "no VPN names from config matched list"
	if !exists {
		reason = "file missing"
	} else if size == 0 {
		reason = "file empty"
	}
	m.Logger.Log(fmt.Sprintf("autostart-check: no saved targets; skipping (list: %s, %s)", url, reason))
}

func (m *Manager) restartAfterStopAll(targets []core.VPNItem) error {
	m.Logger.Event("autostart", "stop_all_restart", [2]string{"targets", strings.Join(vpnItemNames(targets), ",")})
	m.stopAllInternal()
	time.Sleep(350 * time.Millisecond)
	_, _, failures := startBatchWith(targets, func(v core.VPNItem) error { return m.startOneWithRetry(v, 3) })
	if m.NetworkSetup != nil {
		if ssid := m.NetworkSetup.GetCurrentSSID(); ssid != "" {
			m.saveLastSSID(ssid)
		}
		if ip := m.NetworkSetup.GetCurrentIP(); ip != "" {
			m.saveLastIP(ip)
		}
	}
	m.Logger.Log("autostart-check: restarted after stop-all")
	if len(failures) > 0 {
		// Returned, not only logged: this branch is what the hidden
		// `-restart` subcommand runs, and a launchd job or a script that
		// forces a full restart has nothing but the exit status to tell it
		// that a tunnel did not come back.
		m.Logger.Error("autostart-check: did not come back after stop-all: " + strings.Join(failures, "; "))
		return core.StartFailed(strings.Join(failures, "; "))
	}
	return nil
}

// stoppedAutostartTargets is what the autostart check brings back: the
// targets whose sing-box is no longer running.
func (m *Manager) stoppedAutostartTargets(targets []core.VPNItem) []core.VPNItem {
	var failing []core.VPNItem
	for _, v := range targets {
		if v.Type == core.VPNSingbox && !m.IsRunning(v) {
			m.Logger.Warn("autostart-check: singbox target " + v.Name + " not running -> will restart this VPN")
			failing = append(failing, v)
		}
	}
	return failing
}

func (m *Manager) logAutostartResult(restarted []core.VPNItem, failures []string) {
	if len(restarted) > 0 {
		m.Logger.Log("autostart-check: restarted " + strings.Join(vpnItemNames(restarted), ", "))
	}
	if len(failures) > 0 {
		m.Logger.Error("autostart-check: did not come back: " + strings.Join(failures, "; "))
	}
}

func vpnItemNames(items []core.VPNItem) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name)
	}
	return names
}

// networkWaitContext returns a context cancelled when the system goes back to sleep, so a
// WaitForNetworkConnection started on wake does not keep a goroutine blocked for the full timeout
// if the machine sleeps again before the network comes up. The caller must always invoke cancel.
//
// Sleep is detected by a 1s ticker, so cancellation lags a sleep transition by up to ~1s. That is
// negligible against the 30–60s WiFiWaitTimeoutSeconds the wait runs for.
func (m *Manager) networkWaitContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.autoMu.Lock()
				sleeping := m.isSystemSleeping
				m.autoMu.Unlock()
				if sleeping {
					cancel()
					return
				}
			}
		}
	}()
	return ctx, cancel
}

// ScheduleAutostartCheck queues an autostart check, respecting cooldown / sleep state.
func (m *Manager) ScheduleAutostartCheck(reason string, delay time.Duration, fullStopAllFirst, respectQuietHours bool) {
	m.autoMu.Lock()
	defer m.autoMu.Unlock()
	ast := m.Cfg.EffectiveAutostart()
	if respectQuietHours && ast.IsInQuietHours(time.Now()) {
		m.Logger.Debug("schedule-autostart: quiet hours, skipping reason=" + reason)
		return
	}
	if m.isSystemSleeping {
		m.Logger.Debug(fmt.Sprintf("schedule-autostart: system sleeping, queue on wake + fallback in %ds, reason=%s",
			int(ast.FallbackTimeoutSeconds.Seconds()), reason))
		m.hasPendingAutoCheck = true
		if m.pendingAutoCancel != nil {
			close(m.pendingAutoCancel)
		}
		ch := make(chan struct{})
		m.pendingAutoCancel = ch
		go m.awaitMissedWake(ch, ast.FallbackTimeoutSeconds)
		return
	}

	if (strings.Contains(reason, "Network change") || strings.Contains(reason, "SSID change")) && m.hasLastAutostartTime {
		elapsed := time.Since(m.lastAutostartTime)
		if elapsed < ast.CooldownSeconds {
			left := ast.CooldownSeconds - elapsed
			m.Logger.Debug(fmt.Sprintf("schedule-autostart: network change ignored (cooldown, %ds left)", int(left.Seconds())))
			return
		}
	}

	if m.pendingAutoCancel != nil {
		close(m.pendingAutoCancel)
	}
	ch := make(chan struct{})
	m.pendingAutoCancel = ch
	doFullStop := fullStopAllFirst

	m.Logger.Event("autostart", "scheduled",
		[2]string{"delay", fmt.Sprintf("%.1fs", delay.Seconds())},
		[2]string{"reason", reason},
		[2]string{"fullStopAllFirst", fmt.Sprintf("%v", doFullStop)},
	)

	go func() {
		select {
		case <-ch:
			return
		case <-time.After(delay):
		}
		m.runScheduledAutostart(doFullStop, false)
	}()
}

func (m *Manager) awaitMissedWake(ch <-chan struct{}, timeout time.Duration) {
	select {
	case <-ch:
		return
	case <-time.After(timeout):
	}
	m.autoMu.Lock()
	if !m.isSystemSleeping {
		m.autoMu.Unlock()
		return
	}
	m.Logger.Event("autostart", "fallback_wake_missed")
	m.isSystemSleeping = false
	m.hasPendingAutoCheck = false
	m.autoMu.Unlock()
	m.runScheduledAutostart(false, true)
}

func (m *Manager) runScheduledAutostart(fullStop, fallback bool) {
	if err := m.AutostartCheck(false, fullStop); err != nil {
		prefix := "autostart-check failed: "
		if fallback {
			prefix = "autostart-check (fallback) failed: "
		}
		m.Logger.Error(prefix + err.Error())
	}
	// Stamp partial failures too: they still attempted restarts. Wall time
	// prevents a dark-wake stamp from postponing the later real wake.
	if fullStop || fallback {
		m.autoMu.Lock()
		m.lastAutostartTime = time.Now().Round(0)
		m.hasLastAutostartTime = true
		m.autoMu.Unlock()
	}
}

func (m *Manager) hostsToCheck(targets []core.VPNItem) []string {
	set := map[string]struct{}{}
	def := strings.TrimSpace(m.Cfg.Connectivity.DefaultHost)
	if def != "" {
		set[def] = struct{}{}
	}
	for _, v := range targets {
		h := strings.TrimSpace(v.Host)
		if h != "" && strings.ToLower(h) != "null" {
			set[h] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (m *Manager) performPostStartConnectivityChecks(targets []core.VPNItem) {
	// Nothing came up — there is nothing to settle, and nothing to probe.
	if len(targets) == 0 {
		return
	}
	// Short settle, then (for sing-box) dynamic poll until public IP is stable or budget elapses.
	time.Sleep(2 * time.Second)
	hasSingbox := false
	for _, v := range targets {
		if v.Type == core.VPNSingbox {
			hasSingbox = true
			break
		}
	}
	if hasSingbox {
		m.printStableExitIP()
	}
	hosts := m.hostsToCheck(targets)
	if len(hosts) == 0 {
		return
	}
	fmt.Println(ansiutil.Yellow("Connectivity checks:"))
	t := m.Cfg.Connectivity.Timeout
	for _, h := range hosts {
		res, err := webprobe.Get(context.Background(), "http://"+h, webprobe.Options{
			Method:  http.MethodHead,
			Timeout: time.Duration(t) * time.Second,
		})
		ok := err == nil && res.OK()
		httpStr := ansiutil.Red("FAIL")
		if ok {
			httpStr = ansiutil.Green("OK")
		}
		fmt.Println("  " + ansiutil.Yellow(h) + ": http " + httpStr)
		statusStr := "FAIL"
		if ok {
			statusStr = "OK"
		}
		m.Logger.Debug("connectivity: host=" + h + " http=" + statusStr)
	}
}

func (m *Manager) printStableExitIP() {
	ip, country, _ := ipgeo.WaitStable(m.Cfg.EffectiveAPI(), ipgeo.DefaultWaitOptsForPostStart())
	if ip != "" {
		cs := country
		if cs == "" {
			cs = "?"
		}
		fmt.Println(ansiutil.Yellow("Exit IP: " + ip + " (" + cs + ")"))
		m.Logger.Debug("connectivity: exit ip=" + ip + " country=" + cs)
	} else {
		fmt.Println(ansiutil.Yellow("Exit IP: (unable to fetch)"))
	}
}

func (m *Manager) runningVPNs() []core.VPNItem {
	var out []core.VPNItem
	for _, v := range m.Cfg.VPNs {
		if m.IsRunning(v) {
			out = append(out, v)
		}
	}
	return out
}

// Status prints the core process, daemon and autostart status.
func (m *Manager) Status(_ bool) {
	ep := m.Cfg.EffectivePaths()

	// What used to be reported here was the sudoers drop-in, which no longer
	// exists, and "VPN autostart" meaning "is the LaunchDaemon installed" —
	// which now says yes on every machine, because the daemon is the client's
	// privileged component and ships with it. Neither answered the question the
	// user is asking. These two do: is there a daemon to do privileged work,
	// and does it bring tunnels up at boot.
	label := ep.LaunchDaemonLabel
	daemonLoaded := false
	if st, _ := procutil.RunAndCapture(launchctlExecutable, "print", systemDomainPrefix+label); st == 0 {
		daemonLoaded = true
	}
	socket := control.SocketPath(core.DaemonRunDir())
	socketReady := false
	if fi, err := os.Stat(socket); err == nil && fi.Mode()&os.ModeSocket != 0 {
		socketReady = true
	}
	switch {
	case daemonLoaded && socketReady:
		fmt.Println(ansiutil.Green("privileged daemon: running (" + socket + ")"))
	case daemonLoaded:
		fmt.Println(ansiutil.Red("privileged daemon: loaded but not answering (no socket at " + socket + ")"))
	default:
		fmt.Println(ansiutil.Red("privileged daemon: not running"))
	}

	if core.BootAutostartEnabled(m.Paths) {
		fmt.Println(ansiutil.Green("VPN boot autostart: on"))
	} else {
		fmt.Println(ansiutil.Yellow("VPN boot autostart: off"))
	}
	fmt.Println(ansiutil.Yellow(launchd.AppAutostartStatusString(ep.LaunchAgentBarLabel)))
	fmt.Println()

	type vpnStatus struct {
		v       core.VPNItem
		running bool
		pid     int
	}
	statuses := make([]vpnStatus, 0, len(m.Cfg.VPNs))
	for _, v := range m.Cfg.VPNs {
		pid, running := m.runningVPNPID(v)
		if running {
			statuses = append(statuses, vpnStatus{v: v, running: true, pid: pid})
		} else {
			statuses = append(statuses, vpnStatus{v: v, running: false, pid: 0})
		}
	}

	for _, s := range statuses {
		v := s.v
		if s.running {
			fmt.Println(ansiutil.Green("✓ " + v.Name + " connected"))
			m.Logger.Debug(fmt.Sprintf("status: %s [%s] running pid=%d", v.Name, v.Type, s.pid))
		} else {
			fmt.Println(ansiutil.Red("✗ " + v.Name + " stopped"))
			m.Logger.Debug(fmt.Sprintf("status: %s [%s] stopped", v.Name, v.Type))
		}

	}
}

// startTargets, start, stop logic continues in start_stop.go
// resolver application logic in resolvers.go
// daemon loop in daemon.go (with cgo for SCDynamicStore + IOKit)

func (m *Manager) startedListURL() string {
	if m.Cfg.StartedListPath != nil {
		s := strings.TrimSpace(*m.Cfg.StartedListPath)
		if s != "" {
			return s
		}
	}
	return m.Paths.StartedListURL()
}

func (m *Manager) lastSSIDURL() string { return filepath.Join(m.Paths.StateDir(), "last_ssid.txt") }
func (m *Manager) lastIPURL() string   { return filepath.Join(m.Paths.StateDir(), "last_ip.txt") }

func (m *Manager) loadLastSSID() string {
	data, err := os.ReadFile(m.lastSSIDURL())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (m *Manager) saveLastSSID(s string) {
	_ = os.WriteFile(m.lastSSIDURL(), []byte(s+"\n"), 0o644)
}

func (m *Manager) loadLastIP() string {
	data, err := os.ReadFile(m.lastIPURL())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (m *Manager) saveLastIP(s string) {
	_ = os.WriteFile(m.lastIPURL(), []byte(s+"\n"), 0o644)
}

func (m *Manager) saveStartedList(names []string) {
	url := m.startedListURL()
	lowerToName := map[string]string{}
	for _, v := range m.Cfg.VPNs {
		lowerToName[strings.ToLower(v.Name)] = v.Name
	}
	canonical := []string{}
	for _, n := range names {
		key := normalizeStartedListToken(n)
		if can, ok := lowerToName[strings.ToLower(key)]; ok {
			canonical = append(canonical, can)
		}
	}
	uniq := map[string]struct{}{}
	for _, c := range canonical {
		uniq[c] = struct{}{}
	}
	out := make([]string, 0, len(uniq))
	for k := range uniq {
		out = append(out, k)
	}
	sort.Strings(out)
	content := strings.Join(out, "\n") + "\n"
	if err := os.WriteFile(url, []byte(content), 0o644); err != nil {
		m.Logger.Warn("failed to save started list to " + url + ": " + err.Error())
	}
}

func normalizeStartedListToken(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\ufeff", "")
	return s
}

func (m *Manager) loadStartedList() []string {
	data, err := os.ReadFile(m.startedListURL())
	if err != nil {
		return nil
	}
	out := []string{}
	for _, line := range strings.Split(string(data), "\n") {
		t := normalizeStartedListToken(line)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// verifyUserUUID asks the server about a UUID before it is stored; tests swap it.
var verifyUserUUID = telemetry.Verify

// checkUserUUID lets a UUID be stored only when the server knows it, because
// the metrics are counted by it. The check is this machine's first push under
// it, signed as root with the daemon's install ID, so the metrics reach the
// server now rather than at the daemon's next minute.
func (m *Manager) checkUserUUID(uuid string) error {
	if !security.ValidUUID(uuid, false) {
		return core.BadConfig("Invalid user UUID format")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err := verifyUserUUID(ctx, m.Paths.StateDir(), uuid, m.tunnelStates, m.localSingboxVersion)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, telemetry.ErrUnknownUUID):
		return core.BadConfig("This user UUID is not registered on the server")
	case errors.Is(err, telemetry.ErrVerificationUnavailable):
		return core.BadConfig(err.Error())
	default:
		// The error names the URL, never the UUID.
		m.Logger.Warn("User UUID check: " + err.Error())
		return core.BadConfig("Could not reach the server to check the user UUID")
	}
}

// tunnelStates is what telemetry reports: every configured VPN, up or not.
// Read from disk rather than m.Cfg, which in the daemon is the config it
// started with.
func (m *Manager) tunnelStates() map[string]bool {
	up := map[string]bool{}
	if cfg, err := core.ReadConfig(m.Paths); err == nil {
		for _, v := range cfg.VPNs {
			up[v.Name] = m.IsRunning(v)
		}
	}
	return up
}

// splitTagList splits the outbounds line on commas, the separator the menu bar
// sends, and splits a piece on spaces and semicolons as well only when it is
// not a tag the profile offers as it stands. A profile may name a tag "eu west";
// splitting it as well made a tag the panel showed impossible to save.
func splitTagList(raw string, available []string) []string {
	known := make(map[string]struct{}, len(available))
	for _, tag := range available {
		known[strings.ToLower(tag)] = struct{}{}
	}
	out := []string{}
	for _, piece := range strings.Split(raw, ",") {
		piece = strings.TrimSpace(piece)
		if piece == "" {
			continue
		}
		if _, ok := known[strings.ToLower(piece)]; ok {
			out = append(out, piece)
			continue
		}
		out = append(out, strings.FieldsFunc(piece, func(r rune) bool { return r == ' ' || r == ';' })...)
	}
	return out
}

func trimmedInputLines(lines []string) []string {
	cleaned := make([]string, len(lines))
	for i, line := range lines {
		cleaned[i] = strings.TrimSpace(line)
	}
	return cleaned
}

// SetSingboxConfig sets UUID + enabled outbounds, then downloads template.
func (m *Manager) SetSingboxConfig(stdinLines []string) error {
	cleaned := trimmedInputLines(stdinLines)
	uuid, uuidToSave, err := m.validatedConfigUUID(cleaned)
	if err != nil {
		return err
	}
	enabledRaw := ""
	if len(cleaned) > 1 {
		enabledRaw = cleaned[1]
	}
	// Third line: the selector node. A blank line is "no opinion": the config
	// panel sends no node, and overwriting the stored one with an empty string
	// would lose the choice every time somebody saved a UUID.
	var selectedNode *string
	if len(cleaned) > 2 && cleaned[2] != "" {
		node := cleaned[2]
		selectedNode = &node
	}
	// Fourth line: the mode. Blank is "no opinion", like the node above it.
	var naiveMode *bool
	if len(cleaned) > 3 && cleaned[3] != "" {
		on, err := parseSingboxMode(cleaned[3])
		if err != nil {
			return err
		}
		naiveMode = &on
	}
	// A tag the profile does not offer is refused rather than stored: kept, it
	// would match nothing at build time and the choice would be lost silently.
	// Tags are saved as the profile spells them. With no profile downloaded yet
	// there is nothing to check against, and the choice is stored as given —
	// the UUID gate sends the stored answer on a first run.
	available := m.availableSingboxOutbounds()
	tokens := splitTagList(enabledRaw, available)
	enabled, err := validateOutboundChoice(tokens, available)
	if err != nil {
		return err
	}

	// Nothing ticked is not refused here: the config build refuses a profile
	// with no protocol, which is the state that really has nothing to go through.

	// What the user said no to is recorded alongside what they said yes to.
	// Only the "no" is consulted when a config is built, so an outbound the
	// server adds later is on for someone who has used this panel before —
	// with a positive list alone it would have stayed off, unasked.
	disabled := disabledFromChoice(enabled, available)
	// No outbounds line at all is no choice, the way a missing line below it
	// is: `echo uuid | set-singbox-config` used to switch every protocol off.
	// An empty line with no profile to read it against says nothing either.
	// Both save only the UUID and build with what was stored.
	keepStored := len(tokens) == 0 && (len(cleaned) < 2 || len(available) == 0)
	if keepStored {
		enabled, disabled = nil, nil
	}

	if err := routerules.PatchVpnCfgSingboxUUIDAndOutbounds(m.Paths.CfgFile, uuidToSave, enabled, disabled, selectedNode); err != nil {
		m.Logger.Error("Failed to save sing-box uuid/outbounds: " + err.Error())
		return err
	}
	if naiveMode != nil {
		if err := routerules.PatchVpnCfgSingboxNaiveMode(m.Paths.CfgFile, *naiveMode); err != nil {
			m.Logger.Error("Failed to save the sing-box mode: " + err.Error())
			return err
		}
	}
	m.Logger.Log(ansiutil.Green("✓ Saved sing-box uuid/outbounds into " + m.Paths.CfgFile))

	cfg, err := core.ReadConfig(m.Paths)
	if err != nil {
		return err
	}
	m.Cfg = cfg
	// Nothing said about the account here means the stored one still applies.
	if len(cleaned) == 1 {
		return nil
	}
	if uuid == "" {
		uuid = strings.TrimSpace(m.Cfg.Singbox.UUID)
	}
	if keepStored {
		enabled, disabled = m.effectiveEnabledSingboxOutbounds(), m.disabledSingboxOutbounds()
	}

	return m.rebuildSingboxConfigs(uuid, enabled, disabled)
}

// parseSingboxMode reads set-singbox-config's mode line: "naive" for the
// NaiveProxy mode, "standard" for the profile's other protocols.
func parseSingboxMode(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "naive":
		return true, nil
	case "standard":
		return false, nil
	}
	return false, core.BadConfig(`unknown sing-box mode "` + raw + `": use "naive" or "standard"`)
}

func (m *Manager) validatedConfigUUID(cleaned []string) (string, *string, error) {
	uuid := ""
	if len(cleaned) > 0 {
		uuid = cleaned[0]
	}
	// An empty first line is "no opinion", the way the lines below it are:
	// choices can be saved before a UUID has ever been entered.
	var uuidToSave *string
	if uuid != "" {
		uuidToSave = &uuid
	}
	// Every save path ends here — the first-run window, VPN Config, the CLI —
	// so this is where a new UUID has to be one the server knows. The stored
	// one comes back with every save and is not asked about again.
	if uuidToSave != nil && !strings.EqualFold(uuid, strings.TrimSpace(m.Cfg.Singbox.UUID)) {
		if err := m.checkUserUUID(uuid); err != nil {
			return "", nil, err
		}
	}
	return uuid, uuidToSave, nil
}

func (m *Manager) rebuildSingboxConfigs(uuid string, enabled, disabled []string) error {
	targets := m.singboxConfigPathsToUpdate()
	if len(targets) == 0 {
		m.Logger.Warn("singbox config file not found; uuid saved to vpn_cfg.json only.")
		return nil
	}
	for _, p := range targets {
		baseConfigPath := filepath.Join(filepath.Dir(p), "singbox.json")
		root, err := singbox.BuildConfigFromRemoteTemplate(core.SingboxSection{UUID: uuid, EnabledOutbounds: enabled, DisabledOutbounds: disabled, SelectedNode: m.selectedNode(), NaiveMode: m.Cfg.Singbox.NaiveMode}, baseConfigPath, 12*time.Second, m.Logger.Warn)
		if err != nil {
			if uuid == "" {
				// With no account there may be nothing to build yet. What this
				// call saved — the toggles, the node — is in vpn_cfg.json, and
				// the build that follows the first UUID starts from it.
				m.Logger.Warn("Saved, but the sing-box config was not rebuilt: " + err.Error())
				return nil
			}
			m.Logger.Warn("Failed to update sing-box config at " + p + ": " + err.Error())
			return err
		}
		singbox.NormalizeLogOutput(root, m.Paths.LogDir)
		if err := singbox.WriteJSONFileAtomically(root, p); err != nil {
			m.Logger.Warn("Failed to update sing-box config at " + p + ": " + err.Error())
			return err
		}
		m.Logger.Log(ansiutil.Green("✓ Updated sing-box config from remote template at " + p))
	}
	if err := routerules.ReapplySavedRouteRulesToSingboxFiles(m.Paths, m.Cfg); err != nil {
		m.Logger.Warn("Could not re-apply route rules after sing-box template: " + err.Error())
	} else {
		m.Logger.Log(ansiutil.Green("✓ Re-applied route rules / DNS to sing-box after remote template"))
	}
	m.normalizeSingboxConfigs()
	return nil
}

func validateOutboundChoice(tokens, available []string) ([]string, error) {
	known := make(map[string]string, len(available))
	for _, tag := range available {
		known[strings.ToLower(tag)] = tag
	}
	enabled := []string{}
	{
		seen := map[string]struct{}{}
		for _, tok := range tokens {
			key := strings.ToLower(tok)
			tag, ok := tok, true
			if len(available) > 0 {
				tag, ok = known[key]
			}
			if !ok {
				return nil, core.BadConfig("Unknown outbound " + strconv.Quote(tok) + ". Available tags: " + strings.Join(available, ", "))
			}
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			enabled = append(enabled, tag)
		}
	}
	return enabled, nil
}

// SetRouteRules updates direct/processes/paths in vpn_cfg.json + sing-box JSON.
func (m *Manager) SetRouteRules(stdinLines []string) error {
	cleaned := trimmedInputLines(stdinLines)
	getLine := func(i int) string {
		if i < len(cleaned) {
			return cleaned[i]
		}
		return ""
	}
	direct := routerules.ParseDomainList(getLine(0))
	processes := routerules.ParseDomainList(getLine(1))
	pathsTab := getLine(2)
	rawPaths := []string{}
	for _, p := range strings.Split(pathsTab, "\t") {
		t := strings.TrimSpace(p)
		if t != "" {
			rawPaths = append(rawPaths, t)
		}
	}
	bypassMerged := map[string]struct{}{}
	for _, p := range rawPaths {
		for _, exp := range directbypass.ExpandedPathsForExecutable(p) {
			bypassMerged[exp] = struct{}{}
		}
	}
	bypass := make([]string, 0, len(bypassMerged))
	for k := range bypassMerged {
		bypass = append(bypass, k)
	}
	sort.Strings(bypass)

	if err := routerules.PatchVpnCfgRouteRules(m.Paths.CfgFile, direct, processes, bypass); err != nil {
		return err
	}
	m.Logger.Log(ansiutil.Green("✓ Updated vpn_cfg.json route rules"))

	cfg, err := core.ReadConfig(m.Paths)
	if err != nil {
		return err
	}
	m.Cfg = cfg
	if err := routerules.ReapplySavedRouteRulesToSingboxFiles(m.Paths, m.Cfg); err != nil {
		m.Logger.Warn("Could not patch sing-box route rules: " + err.Error())
	} else {
		m.Logger.Log(ansiutil.Green("✓ Updated sing-box JSON DNS and route rules on all sing-box config paths"))
	}
	m.normalizeSingboxConfigs()
	return nil
}

// AutostartStatus prints LaunchDaemon status.
func (m *Manager) AutostartStatus() {
	label := m.Cfg.EffectivePaths().LaunchDaemonLabel
	plistPath := launchd.PlistPath(label)
	m.Logger.Log("Autostart (LaunchDaemon) status for " + label)
	_, err := os.Stat(plistPath)
	if err == nil {
		m.Logger.Log(ansiutil.Green("  ✓ Plist exists at " + plistPath))
	} else {
		m.Logger.Warn("  ✗ Plist not found at " + plistPath)
	}
	st, out := procutil.RunAndCapture(launchctlExecutable, "print", systemDomainPrefix+label)
	if st != 0 {
		m.Logger.Warn(fmt.Sprintf("  ✗ Service not loaded (launchctl print exit=%d)", st))
	} else {
		m.logDaemonFields(out)
	}
	if data, err := os.ReadFile(plistPath); err == nil {
		m.logPlistFields(data)
	}
	if st2, lstOut := procutil.RunAndCapture(launchctlExecutable, "list"); st2 == 0 {
		hasEntry := strings.Contains(lstOut, label)
		if hasEntry {
			m.Logger.Log(ansiutil.Green("  ✓ launchctl list: entry present"))
		} else {
			m.Logger.Warn("  ✗ launchctl list: no entry")
		}
	}
	if err != nil || st != 0 {
		m.Logger.Warn("Hint: run 'sudo jorbique-vpn install-autostart' to install and load the LaunchDaemon.")
	}
}

func (m *Manager) logPlistFields(data []byte) {
	parts := make([]string, 0, 3)
	for _, field := range []struct{ key, name string }{
		{"ProgramArguments", "ProgramArguments"}, {"StandardOutPath", "StdOut"}, {"StandardErrorPath", "StdErr"},
	} {
		mark := " ✗"
		if strings.Contains(string(data), "<key>"+field.key+"</key>") {
			mark = " ✓"
		}
		parts = append(parts, field.name+mark)
	}
	m.Logger.Log("  • Plist fields: " + strings.Join(parts, ", "))
}

func (m *Manager) logDaemonFields(out string) {
	findLine := func(prefix string) string {
		for _, l := range strings.Split(out, "\n") {
			t := strings.TrimSpace(l)
			if strings.HasPrefix(t, prefix) {
				return t
			}
		}
		return ""
	}
	for _, prefix := range []string{"enabled =", "state =", "pid =", "last exit status ="} {
		if line := findLine(prefix); line != "" {
			m.Logger.Log("  • " + line)
		} else if prefix == "pid =" {
			m.Logger.Warn("  • pid not present (not running)")
		}
	}
}

func (m *Manager) InstallAutostart() error {
	if !DaemonModeSupported() {
		return core.UnsupportedBuild(
			"This build has no system daemon support (cross-compiled without CGO).",
			"Use per-user startup: jorbique-vpn install-app-autostart. For LaunchDaemon (--daemon), use a macOS (darwin) build.",
		)
	}
	// The marker first, and the daemon only if it is missing. This runs as a
	// child of the daemon — the menu bar asks it over the control socket — and
	// installing a LaunchDaemon boots the job out, which kills the process
	// doing the installing along with the daemon it was going to bring back.
	// The daemon reads the marker on every event, so there is nothing to
	// restart for it to take effect.
	if err := core.SetBootAutostart(m.Paths, true); err != nil {
		return err
	}
	if !m.DaemonLoaded() {
		if err := m.EnsureDaemonInstalled(); err != nil {
			return err
		}
	}
	m.Logger.Log("boot autostart enabled")
	return nil
}

// UninstallAutostart stops the daemon bringing tunnels up at boot. The daemon
// itself keeps running — uninstalling it would take the privileged operations
// of the menu bar with it.
func (m *Manager) UninstallAutostart() error {
	if err := core.SetBootAutostart(m.Paths, false); err != nil {
		return err
	}
	m.Logger.Log("boot autostart disabled")
	return nil
}

// DaemonLoaded reports whether launchd already has the job.
func (m *Manager) DaemonLoaded() bool {
	label := m.Cfg.EffectivePaths().LaunchDaemonLabel
	st, _ := procutil.RunAndCapture(launchctlExecutable, "print", systemDomainPrefix+label)
	return st == 0
}

// EnsureDaemonInstalled writes and loads the LaunchDaemon when it is missing.
// Called from the installer and from anything that needs the daemon there.
//
// It reloads the job, so it must not be called from a process the daemon
// started: the bootout would kill the caller. Check daemonLoaded first.
func (m *Manager) EnsureDaemonInstalled() error {
	if !DaemonModeSupported() {
		return core.UnsupportedBuild(
			"This build has no system daemon support (cross-compiled without CGO).",
			"Use per-user startup: jorbique-vpn install-app-autostart. For LaunchDaemon (--daemon), use a macOS (darwin) build.",
		)
	}
	return launchd.Install(m.Paths.ExecutablePath(), m.Cfg.EffectivePaths().LaunchDaemonLabel, m.Logger)
}

// RefreshRemoteProfile downloads the sing-box profile into the cache beside the
// config.
//
// The menu bar cannot do this itself: the config directory belongs to root and
// the bar runs as the user, so its write was dropped and the panels went on
// saying the profile had never been downloaded. The daemon owns that directory,
// and this is the subcommand the bar asks it through.
func (m *Manager) RefreshRemoteProfile() error {
	for _, vpn := range m.Cfg.VPNs {
		if vpn.Type != core.VPNSingbox || strings.TrimSpace(vpn.File) == "" {
			continue
		}
		path := m.Paths.ResolveFromCfg(vpn.File)
		if err := singbox.RefreshRemoteTemplate(path, 20*time.Second, m.Logger.Warn); err != nil {
			return err
		}
		m.Logger.Log(ansiutil.Green("✓ Downloaded the sing-box profile beside " + path))
		return nil
	}
	return core.BadConfig("no sing-box VPN is configured")
}

func (m *Manager) selectedNode() string {
	return strings.TrimSpace(m.Cfg.Singbox.SelectedNode)
}

// disabledSingboxOutbounds is what the user turned off, with the blanks
// removed. nil means the configuration was written before this was recorded,
// and the older positive list decides instead.
func (m *Manager) disabledSingboxOutbounds() []string {
	if m.Cfg.Singbox.DisabledOutbounds == nil {
		return nil
	}
	out := []string{}
	for _, t := range m.Cfg.Singbox.DisabledOutbounds {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// availableSingboxOutbounds is what the downloaded profile offers, read from
// the cache next to the sing-box config.
func (m *Manager) availableSingboxOutbounds() []string {
	for _, v := range m.Cfg.VPNs {
		if v.Type == core.VPNSingbox && strings.TrimSpace(v.File) != "" {
			return singbox.AvailableProxyTags(v.File)
		}
	}
	return nil
}

// disabledFromChoice turns "these are on" into "these are off", against what
// the profile currently offers. With no profile it records nothing: inventing a
// "no" for an outbound nobody has seen would turn it off for good.
func disabledFromChoice(enabled, available []string) []string {
	if len(available) == 0 {
		return nil
	}
	on := make(map[string]struct{}, len(enabled))
	for _, t := range enabled {
		on[strings.ToLower(strings.TrimSpace(t))] = struct{}{}
	}
	off := []string{}
	for _, t := range available {
		if _, ok := on[strings.ToLower(t)]; !ok {
			off = append(off, t)
		}
	}
	return off
}
