package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"jorbiquevpn/internal/config"
	"jorbiquevpn/internal/constants"
	"jorbiquevpn/internal/core"
	"jorbiquevpn/internal/dashboard"
	"jorbiquevpn/internal/ipinfo"
	"jorbiquevpn/internal/logging"
	"jorbiquevpn/internal/network"
	"jorbiquevpn/internal/paths"
	"jorbiquevpn/internal/security"
	"jorbiquevpn/internal/semver"
	"jorbiquevpn/internal/server"
	"jorbiquevpn/internal/sysexec"
	"jorbiquevpn/internal/telemetry"
	"jorbiquevpn/internal/update"
	"jorbiquevpn/internal/utils"
)

const (
	controllerLogFile  = "controller.log"
	currentProfileFile = ".current_profile"
)

var singboxSemverRe = regexp.MustCompile(`(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?)`)

// Cache sing-box --version output: UI polls every few seconds; avoid spawning exe repeatedly.
var (
	localVersionMu    sync.Mutex
	localVersionPath  string
	localVersionMtime time.Time
	localVersionValue string
)

type App struct {
	dataDir   string
	store     *server.Store
	cleanupMu sync.Mutex // Connect/Disconnect full cleanup; Recover uses TryLock to avoid races
}

func New() *App {
	dataDir := paths.DataDir()
	if err := paths.EnsureDataDirs(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "%s: data directories: %v\n", constants.AppDisplayName, err)
	}
	logging.Initialize(controllerLogFile, dataDir) // ensure controller.log exists from first run
	store, err := server.Load(dataDir)
	if err != nil {
		logging.Write(err.Error()+" — starting with empty server list", "WARNING")
		store = &server.Store{}
	}
	a := &App{dataDir: dataDir, store: store}
	go func() {
		_, _ = config.FetchSingboxClientJSONOrCache(nil)
	}()
	return a
}

func warnConfigInit(err error) {
	if err != nil {
		logging.Write("Configuration: "+err.Error()+" — using defaults", "WARNING")
	}
}

func (a *App) GetDataDir() string {
	// dataDir is set once in New and never mutated; no lock needed for reads.
	return a.dataDir
}

func (a *App) IsConnected() bool {
	return core.IsRunning()
}

// StartTelemetry reports once a minute whether the tunnel is up (see internal/telemetry).
func (a *App) StartTelemetry() {
	logFn := controllerLog(a.GetDataDir(), false)
	telemetry.Start(a.GetDataDir(), a.store.GetUserUUID, a.tunnelStates, a.GetSingboxLocalVersion,
		func(msg string) { logFn(msg, "INFO") })
}

func (a *App) tunnelStates() map[string]bool {
	return map[string]bool{"sing-box": a.IsConnected()}
}

// HasUserUUID reports whether a user UUID is stored. Until one is, the window
// shows only the UUID screen and Connect refuses.
func (a *App) HasUserUUID() bool {
	return security.ValidUUID(a.store.GetUserUUID(), false)
}

// verifyUserUUID asks the server about a UUID before it is stored; tests swap it.
var verifyUserUUID = telemetry.Verify

const userUUIDCheckTimeout = 20 * time.Second

// checkUserUUID lets a UUID be stored only when the server knows it, because
// the metrics are counted by it. The check is the user's first metrics push,
// so they reach the server now rather than a minute later.
func (a *App) checkUserUUID(uuid string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), userUUIDCheckTimeout)
	defer cancel()
	err := verifyUserUUID(ctx, a.GetDataDir(), uuid, a.tunnelStates, a.GetSingboxLocalVersion)
	switch {
	case err == nil:
		return "", nil
	case errors.Is(err, telemetry.ErrUnknownUUID):
		return "This user UUID is not registered on the server", ErrUnknownUUID
	case errors.Is(err, telemetry.ErrVerificationUnavailable):
		return "", err
	default:
		// The error names the URL, never the UUID.
		controllerLog(a.GetDataDir(), false)("User UUID check: "+err.Error(), logging.LevelWarn)
		return "Could not reach the server to check the user UUID", ErrServerUnreachable
	}
}

// ActivateUserUUID stores the UUID the first-run screen asks for, once the
// server has confirmed it.
func (a *App) ActivateUserUUID(userUUID string) (msg string, err error) {
	uuid := strings.TrimSpace(userUUID)
	if !security.ValidUUID(uuid, false) {
		return "Invalid user UUID format", ErrInvalidVless
	}
	if msg, err := a.checkUserUUID(uuid); err != nil {
		return msg, err
	}
	a.store.SetUserUUIDAndOutbounds(uuid, nil)
	a.store.SetConfigSavedAt(time.Now().Unix())
	if err := a.store.Save(a.GetDataDir()); err != nil {
		return "Failed to save config", ErrConfigUpdate
	}
	removeVpnFormDraft(a.GetDataDir())
	return "", nil
}

// GetCurrentStackMode returns the current stack mode with proper capitalization: System, Gvisor, Mixed
func (a *App) GetCurrentStackMode() string {
	data, err := os.ReadFile(filepath.Join(a.GetDataDir(), currentProfileFile))
	if err != nil {
		// A disconnect removes that file, so with nothing running this fell back
		// to a dash — while the macOS window kept showing the stack it would
		// come up on. The saved preference is that stack: the last one that
		// started, and the one the next connect will use.
		return capitalizeStackMode(a.store.GetPreferredStackMode())
	}
	return capitalizeStackMode(string(data))
}

// capitalizeStackMode renders a stack name the way the window shows it, from
// either a profile file ("tun_gvisor") or a stored preference ("gvisor").
func capitalizeStackMode(value string) string {
	s := strings.TrimSpace(value)
	switch {
	case strings.Contains(strings.ToLower(s), "system"):
		return "System"
	case strings.Contains(strings.ToLower(s), "gvisor"):
		return "Gvisor"
	case strings.Contains(strings.ToLower(s), "mixed"):
		return "Mixed"
	}
	if s != "" {
		return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
	}
	return s
}

// httpClientNoKeepAlive is what the exit-address lookup goes out on. Keep-alive
// is off so a connection opened before the tunnel came up is never reused for a
// question about the route the tunnel replaced.
var httpClientNoKeepAlive = &http.Client{
	Timeout:   4 * time.Second,
	Transport: &http.Transport{DisableKeepAlives: true},
}

// ipInfoTTL is how long an exit-address lookup stands before the card asks
// again. The public services rate-limit, and the address only changes when the
// tunnel does — which is when the card asks with force.
const ipInfoTTL = 5 * time.Minute

var ipInfoCache = ipinfo.NewCache(ipInfoTTL, httpClientNoKeepAlive)

// GetIPInfo answers the IP-information card: where the traffic comes out, and
// the provider and place it appears to come from. force is the refresh button
// and the moment a tunnel goes up or down.
func (a *App) GetIPInfo(force bool) (ipinfo.Info, error) {
	return ipInfoCache.Get(context.Background(), force)
}

func (a *App) Connect(stackMode string) (msg string, err error) {
	// Every connect ends here — the window, the tray and the health loop — so
	// this is what keeps the app closed until the first-run screen has a UUID.
	if !a.HasUserUUID() {
		return "Enter your user UUID first", ErrVlessRequired
	}
	a.cleanupMu.Lock()
	defer a.cleanupMu.Unlock()
	// The exit address is about to be a different one.
	ipInfoCache.Invalidate()

	dataDir := a.GetDataDir()
	logFn := controllerLog(dataDir, true)

	// Ensure sing-box exists (download if missing)
	singboxExe, err := update.EnsureSingbox(dataDir, logFn)
	if err != nil {
		return "Failed to get sing-box: " + err.Error(), ErrSingboxNotFound
	}
	// A `naive` outbound is a naiveproxy client, which sing-box loads out of a
	// Chromium library it does not ship, from the directory it runs in — so the
	// library is installed alongside the binary above. Not fatal on its own:
	// only a profile that actually uses `naive` needs it, and sing-box reports
	// that far more precisely than a guess made here would.
	if _, cronetErr := update.EnsureCronet(dataDir, logFn); cronetErr != nil {
		logFn("cronet library unavailable: "+cronetErr.Error()+" — a profile using the naive outbound will not start", "WARN")
	}

	// Load config from embedded singbox_cfg.yml (or external override if present)
	projectRoot := utils.GetProjectRoot()
	cfgName, cfgRoot := "", ""
	if projectRoot != "" {
		if _, err := os.Stat(filepath.Join(projectRoot, "singbox_cfg.yml")); err == nil {
			cfgName, cfgRoot = "singbox_cfg.yml", projectRoot
		}
	}
	cfg, errInit := config.Initialize(cfgName, cfgRoot) // no external file: the embedded copy
	warnConfigInit(errInit)

	remoteJSON, err := config.FetchSingboxClientJSONOrCache(func(msg string) { logFn(msg, "WARN") })
	if err != nil {
		return "Failed to load VPN profile from server: " + err.Error(), ErrConfigUpdate
	}

	configPath, err := a.buildRuntimeConfig(stackMode, remoteJSON)
	if err != nil {
		if errors.Is(err, ErrVlessRequired) {
			return "A valid user UUID is required. Open VPN Configuration and enter your UUID.", err
		}
		return "Config build failed: " + err.Error(), ErrConfigUpdate
	}

	var stackName string
	switch stackMode {
	case "system":
		stackName = "Tun System"
	case "gvisor":
		stackName = "Tun Gvisor"
	case "mixed":
		stackName = "Tun Mixed"
	default:
		return "Invalid stack mode", ErrInvalidStackMode
	}

	// Win10: a singtun adapter left by a prior crash/reboot/unclean exit makes the first
	// sing-box run fail with "file already exists" (works on retry only because the failed
	// attempt cleans it up below). Clear it up front so the first Connect succeeds.
	network.PrepareSingtunForConnect(network.GetTunInterfaceNameFromConfig(configPath), logFn)

	success, errMsg := core.Start(configPath, stackName, dataDir, singboxExe, cfg, logFn)

	if !success {
		network.RemoveNrptDnsCapture(logFn)
		network.PrepareSingtunForConnect(
			network.GetTunInterfaceNameFromConfig(configPath),
			logFn,
		)
		if errMsg != "" {
			return errMsg, ErrStartupFailed
		}
		return "Startup failed. Check logs for details.", ErrStartupFailed
	}
	a.store.SetPreferredStackMode(stackMode)
	if err := a.store.Save(a.GetDataDir()); err != nil {
		logFn("Could not save the stack: "+err.Error(), logging.LevelWarn)
	}
	return "", nil
}

func (a *App) buildRuntimeConfig(stackMode string, remoteJSON []byte) (string, error) {
	userUUID, enabled, directDomains := a.effectiveVpnUserForConnect()
	available := config.ProxyTagsFromJSON(remoteJSON)
	if gone := a.vanishedOutbounds(enabled, available); len(gone) > 0 {
		logging.Write("sing-box profile: the server no longer offers "+strings.Join(gone, ", "), logging.LevelWarn)
	}
	if !security.ValidUUID(strings.TrimSpace(userUUID), false) {
		return "", ErrVlessRequired
	}
	// The builder checks for a usable tunnel after applying the saved choices.
	return config.BuildRuntimeConfig(stackMode, remoteJSON, config.RuntimeOptions{
		UserUUID:            strings.TrimSpace(userUUID),
		OutboundEnabled:     enabled,
		IgnoredProcessNames: a.store.GetIgnoredProcessNames(),
		IgnoredProcessPaths: a.store.GetIgnoredProcessPaths(),
		SelectedNode:        a.store.GetSelectedNode(),
		UserDirectDomains:   directDomains,
	})
}

// GetPreferredStackMode returns the last saved TUN stack (system, gvisor, mixed) or empty.
func (a *App) GetPreferredStackMode() string {
	return a.store.GetPreferredStackMode()
}

// ConnectSavedStack connects using persisted stack. openUI is true when VPN config or stack preference is missing.
func (a *App) ConnectSavedStack() (openUI bool, err error) {
	if !a.HasVpnUserConfig() {
		return true, nil
	}
	mode := a.GetPreferredStackMode()
	if mode == "" {
		return true, nil
	}
	_, err = a.Connect(mode)
	return false, err
}

// hasSomethingToRouteThrough reports whether a protocol is left to route
// through. With no profile downloaded yet there is nothing to check against,
// so the stored answers are taken at face value — refusing there would block
// the settings window on a first run with no network.
func hasSomethingToRouteThrough(enabled map[string]bool, available []string) bool {
	if len(available) == 0 {
		return true
	}
	for _, t := range available {
		if config.OutboundEnabled(enabled, t) {
			return true
		}
	}
	return false
}

func (a *App) Disconnect() (string, error) {
	a.cleanupMu.Lock()
	defer a.cleanupMu.Unlock()
	// The exit address is about to be a different one.
	ipInfoCache.Invalidate()

	dataDir := a.GetDataDir()
	logFn := controllerLog(dataDir, false)

	core.Stop(logFn)
	waitForTunGone()
	// Clear any leftover singtun adapter on disconnect too (same operation as the connect-time prep).
	network.PrepareSingtunForConnect(
		network.GetTunInterfaceNameFromConfig(paths.RuntimeConfigPath()),
		logFn,
	)

	a.cleanupTunnelArtifacts(dataDir, logFn)
	return "", nil
}

func controllerLog(dataDir string, ensureDirs bool) func(string, string) {
	logging.Initialize(controllerLogFile, dataDir)
	if ensureDirs {
		if err := paths.EnsureDataDirs(); err != nil {
			logging.Write("Could not ensure data directories: "+err.Error(), logging.LevelWarn)
		}
	}
	return logging.Write
}

func waitForTunGone() {
	deadline := time.Now().Add(800 * time.Millisecond)
	for time.Now().Before(deadline) {
		_, _, ok := network.FindTunInterface("", constants.TunIPRange)
		if !ok {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (a *App) staleTunnelArtifactsPresent(dataDir string) bool {
	if network.LoadState(dataDir) {
		return true
	}
	if _, err := os.Stat(filepath.Join(dataDir, currentProfileFile)); err == nil {
		return true
	}
	return false
}

// cleanupTunnelArtifacts undoes the DNS changes and removes session files after
// sing-box is already stopped. Routes are not among them: sing-box owns those and
// takes them down with itself.
func (a *App) cleanupTunnelArtifacts(dataDir string, logFn func(string, string)) {
	// Unconditional (not gated on persisted state): a stale NRPT catch-all with the
	// TUN gone would leave the machine without DNS entirely.
	network.RemoveNrptDnsCapture(logFn)
	network.ClearState(dataDir)
	network.ClearDnsCache()
	_ = os.Remove(filepath.Join(dataDir, currentProfileFile))
	// The sing-box cache dir (persistent DNS cache) deliberately survives
	// disconnects; CleanupSingboxCacheOnExit removes it at full app shutdown.
}

// RecoverFromUnexpectedExit removes routes and session files when sing-box died (crash, kill) or the app
// ended after an unclean shutdown, so traffic is not left pointing at a dead TUN gateway.
func (a *App) RecoverFromUnexpectedExit() (string, error) {
	dataDir := a.GetDataDir()
	if core.IsRunning() {
		return "", nil
	}
	// Win10: tasklist-based checks could false-negative while sing-box still holds cache.db.
	if core.ResyncSingboxIfAlive(dataDir) {
		return "", nil
	}
	if !a.cleanupMu.TryLock() {
		return "", nil
	}
	defer a.cleanupMu.Unlock()

	if core.IsRunning() {
		return "", nil
	}
	if core.ResyncSingboxIfAlive(dataDir) {
		return "", nil
	}
	logFn := controllerLog(dataDir, false)
	if a.staleTunnelArtifactsPresent(dataDir) {
		logFn("sing-box is not running but VPN session artifacts remain — cleaning up DNS", "WARNING")
		waitForTunGone()
		a.cleanupTunnelArtifacts(dataDir, logFn)
		network.PrepareSingtunForConnect(
			network.GetTunInterfaceNameFromConfig(paths.RuntimeConfigPath()),
			logFn,
		)
	}
	return "", nil
}

func removeConfigCache(dataDir string, logFn func(string, string)) {
	cacheDir := filepath.Join(dataDir, "cache")
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		return
	}
	if err := os.RemoveAll(cacheDir); err != nil && logFn != nil {
		logFn("Failed to remove cache: "+err.Error(), "WARNING")
	}
}

// CleanupSingboxCacheOnExit removes the sing-box cache dir (its persistent DNS
// cache) at full app shutdown — the only place it is deleted; disconnects keep
// it so reconnects start with a warm cache. Skipped while sing-box still runs:
// the tunnel deliberately survives closing the UI, and sing-box holds cache.db
// open, so removal would fail (Windows) or yank the cache from a live tunnel.
func (a *App) CleanupSingboxCacheOnExit() {
	if core.IsRunning() {
		return
	}
	dataDir := a.GetDataDir()
	removeConfigCache(dataDir, controllerLog(dataDir, false))
}

// vanishedOutbounds lists the outbounds the user had on that the profile no
// longer offers, so the log can name what a server-side rename took away.
// effective is what the build uses; the store already drops tags the profile
// lacks from it, so the saved answers are read raw underneath. A profile with
// no proxy outbounds at all is not a rename: nothing is named.
func (a *App) vanishedOutbounds(effective map[string]bool, available []string) []string {
	if len(available) == 0 {
		return nil
	}
	had := a.store.OutboundEnabledFor(nil)
	maps.Copy(had, effective)
	var gone []string
	for tag, on := range had {
		if on && !slices.Contains(available, tag) {
			gone = append(gone, tag)
		}
	}
	sort.Strings(gone)
	return gone
}

// normalizeOutboundEnabledForSave fills in an answer for every outbound the
// profile offers: unanswered means on. With no profile downloaded yet it saves
// what it was given rather than throwing the user's choices away.
func normalizeOutboundEnabledForSave(in map[string]bool, available []string) map[string]bool {
	if len(available) == 0 {
		out := make(map[string]bool, len(in))
		for k, v := range in {
			out[k] = v
		}
		return out
	}
	out := make(map[string]bool, len(available))
	for _, t := range available {
		out[t] = config.OutboundEnabled(in, t)
	}
	return out
}

// SetVpnUserConfig validates and saves user UUID, outbound toggles, and/or direct-domain list (one hostname per line).
// SetVpnUserConfig saves the account and the outbound choices. Direct domains
// used to be saved here too; they moved to the ignore window, where they sit
// next to the process exceptions they are a kind of.
func (a *App) SetVpnUserConfig(userUUID string, outboundEnabled map[string]bool) (msg string, err error) {
	uuid := strings.TrimSpace(userUUID)
	// Every user has one now, whatever they route through: the metrics are
	// counted by it, and clearing it would lock the app behind the first-run
	// screen again.
	if uuid == "" {
		return "Enter a user UUID", ErrInvalidVless
	}
	if !security.ValidUUID(uuid, false) {
		return "Invalid user UUID format", ErrInvalidVless
	}
	if !strings.EqualFold(uuid, strings.TrimSpace(a.store.GetUserUUID())) {
		if msg, err := a.checkUserUUID(uuid); err != nil {
			return msg, err
		}
	}
	// Whether anything is left to route through is not this call's business:
	// Connect checks it, with the profile and the whole saved state in hand.

	available := config.AvailableProxyTags()
	if len(available) == 0 {
		// No profile means the window drew no toggles, so a tag missing from
		// the map is not an answer: keep what was stored for it.
		stored := a.store.OutboundEnabledFor(nil)
		for tag, on := range outboundEnabled {
			stored[tag] = on
		}
		outboundEnabled = stored
	}
	merged := normalizeOutboundEnabledForSave(outboundEnabled, available)
	a.store.SetUserUUIDAndOutbounds(uuid, merged)
	a.store.SetConfigSavedAt(time.Now().Unix())
	if err := a.store.Save(a.GetDataDir()); err != nil {
		return "Failed to save config", ErrConfigUpdate
	}
	removeVpnFormDraft(a.GetDataDir())
	return "", nil
}

func (a *App) GetLogContent(logName string) (string, error) {
	if logName == "" {
		logName = controllerLogFile
	}
	base := filepath.Base(strings.ReplaceAll(logName, "\\", "/"))
	if base == "." || base == ".." || strings.Contains(base, "..") {
		return "", errors.New("invalid log file name")
	}
	logsDir := filepath.Join(a.GetDataDir(), "logs")
	logPath := filepath.Join(logsDir, base)
	if !security.SecurePath(logPath, logsDir, false) {
		return "", errors.New("invalid log path")
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (a *App) ListLogFiles() ([]string, error) {
	logsDir := filepath.Join(a.GetDataDir(), "logs")
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

func (a *App) dashboardClient() (*dashboard.Client, error) {
	return dashboard.NewClient(paths.RuntimeConfigPath())
}

func (a *App) DashboardSnapshot(ctx context.Context) (dashboard.Snapshot, error) {
	client, err := a.dashboardClient()
	if err != nil {
		return dashboard.Snapshot{}, err
	}
	return client.Snapshot(ctx)
}

func (a *App) DashboardSelectOutbound(ctx context.Context, group, outbound string) (err error) {
	client, err := a.dashboardClient()
	if err != nil {
		return err
	}
	return client.SelectOutbound(ctx, group, outbound)
}

func (a *App) DashboardURLTest(ctx context.Context, outbound string) (err error) {
	client, err := a.dashboardClient()
	if err != nil {
		return err
	}
	return client.URLTest(ctx, outbound)
}

// ClearLogs removes all log files
func (a *App) ClearLogs() error {
	logging.ClearAll(a.GetDataDir())
	return nil
}

// VpnUserConfigForm holds VPN modal fields for the Wails frontend.
type VpnUserConfigForm struct {
	UUID            string          `json:"uuid"`
	DirectDomains   string          `json:"directDomains"`
	OutboundEnabled map[string]bool `json:"outboundEnabled"`
}

// GetVpnUserConfig returns UUID, toggles, and direct-domain block — prefers a newer auto-saved draft over last official save.
func (a *App) GetVpnUserConfig() VpnUserConfigForm {
	base := VpnUserConfigForm{
		UUID:            a.store.GetUserUUID(),
		DirectDomains:   strings.Join(a.store.GetDirectDomains(), "\n"),
		OutboundEnabled: a.store.GetOutboundEnabled(),
	}
	savedAt := a.store.GetConfigSavedAt()
	draft, err := readVpnFormDraft(a.GetDataDir())
	if err != nil || draft == nil || draft.UpdatedAt <= savedAt {
		return base
	}
	if strings.TrimSpace(draft.UUID) == "" && strings.TrimSpace(draft.DirectDomains) == "" && len(draft.OutboundEnabled) == 0 {
		return base
	}
	if strings.TrimSpace(draft.UUID) != "" {
		base.UUID = strings.TrimSpace(draft.UUID)
	}
	if strings.TrimSpace(draft.DirectDomains) != "" {
		base.DirectDomains = draft.DirectDomains
	}
	if len(draft.OutboundEnabled) > 0 {
		// The draft answers for the tags the window was showing; anything else
		// keeps what was saved. Both sides come from the profile, so a tag that
		// disappeared between opening the window and saving simply goes.
		out := make(map[string]bool, len(base.OutboundEnabled))
		for t, v := range base.OutboundEnabled {
			out[t] = v
		}
		for t, v := range draft.OutboundEnabled {
			out[t] = v
		}
		base.OutboundEnabled = out
	}
	return base
}

// HasVpnUserConfig returns true when the client has enough to connect with: the
// account the protocols authenticate with, and a protocol to route through.
func (a *App) HasVpnUserConfig() bool {
	stored := a.store.GetOutboundEnabled()
	if configuredEnough(a.store.GetUserUUID()) && hasSomethingToRouteThrough(stored, config.AvailableProxyTags()) {
		return true
	}
	draft, err := readVpnFormDraft(a.GetDataDir())
	if err != nil || draft == nil || draft.UpdatedAt <= a.store.GetConfigSavedAt() {
		return false
	}
	storedOutbound := a.store.GetOutboundEnabled()
	en := storedOutbound
	if len(draft.OutboundEnabled) > 0 {
		en = make(map[string]bool, len(storedOutbound))
		for t, v := range storedOutbound {
			en[t] = v
		}
		for t, v := range draft.OutboundEnabled {
			en[t] = v
		}
	}
	uuid := strings.TrimSpace(a.store.GetUserUUID())
	if strings.TrimSpace(draft.UUID) != "" {
		uuid = strings.TrimSpace(draft.UUID)
	}
	return configuredEnough(uuid) && hasSomethingToRouteThrough(en, config.AvailableProxyTags())
}

// configuredEnough reports whether the account side of the configuration is
// settled: a valid UUID.
func configuredEnough(uuid string) bool {
	return security.ValidUUID(strings.TrimSpace(uuid), false)
}

// GetAvailableOutbounds lists the outbound tags the downloaded sing-box profile
// offers, for the settings window to draw a row per protocol. Empty means no
// profile has been fetched yet, and the window says so rather than showing a
// list of protocols this build happens to know the names of.
func (a *App) GetAvailableOutbounds() []string {
	// Downloaded, not read off disk: the window is opened to see what the
	// server offers, and the cache only changes when a connection is made. A
	// failed download falls back to the cache inside this call.
	if tags := config.AvailableProxyTagsFresh(); len(tags) > 0 {
		return tags
	}
	return config.AvailableProxyTags()
}

// GetDirectDomains returns the extra domains that bypass the VPN, one per line.
func (a *App) GetDirectDomains() string {
	return strings.Join(a.store.GetDirectDomains(), "\n")
}

// SetDirectDomains saves those domains. They live with the ignored processes
// rather than with the account: both are exceptions to the tunnel, and one
// window for "what should not go through the VPN" beats two.
func (a *App) SetDirectDomains(block string) (msg string, err error) {
	list, err := config.ParseAndValidateDirectDomains(block)
	if err != nil {
		return "Direct domains: " + err.Error(), ErrInvalidVless
	}
	a.store.SetDirectDomains(list)
	if err := a.store.Save(a.GetDataDir()); err != nil {
		return "Failed to save direct domains", ErrConfigUpdate
	}
	return "", nil
}

// ListActiveApps returns a best-effort list of currently active apps (Windows: processes with a main window).
func (a *App) ListActiveApps() (processes []utils.ActiveProcess, err error) {
	return utils.ListActiveApps()
}

// GetIgnoredProcessNames returns the current ignore list (matched by sing-box `process_name`).
func (a *App) GetIgnoredProcessNames() []string {
	return a.store.GetIgnoredProcessNames()
}

// GetIgnoredProcessPaths returns full executable paths for sing-box `process_path` rules.
func (a *App) GetIgnoredProcessPaths() []string {
	return a.store.GetIgnoredProcessPaths()
}

func normalizeBypassPaths(paths []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		p = filepath.Clean(p)
		if !filepath.IsAbs(p) {
			return nil, fmt.Errorf("path must be absolute: %q", p)
		}
		lp := strings.ToLower(p)
		if strings.Contains(lp, "..") {
			return nil, fmt.Errorf("invalid path: %q", p)
		}
		if runtime.GOOS == "windows" {
			if !strings.HasSuffix(lp, ".exe") {
				return nil, fmt.Errorf("only .exe files are allowed: %q", p)
			}
		}
		if seen[lp] {
			continue
		}
		seen[lp] = true
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out, nil
}

// SetIgnoredProcessNamesAndPaths saves both process_name and process_path bypass lists.
func (a *App) SetIgnoredProcessNamesAndPaths(names []string, paths []string) (err error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		key := strings.ToLower(n)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })

	outPaths, err := normalizeBypassPaths(paths)
	if err != nil {
		return err
	}
	a.store.SetIgnoredProcessNames(out)
	a.store.SetIgnoredProcessPaths(outPaths)
	return a.store.Save(a.GetDataDir())
}

func (a *App) IsSingboxInstalled() bool {
	exePath := filepath.Join(a.GetDataDir(), constants.ProcessExeName)
	_, err := os.Stat(exePath)
	return err == nil
}

// GetSingboxLocalVersion returns version of installed sing-box, or "" if not installed
func (a *App) GetSingboxLocalVersion() string {
	exePath := filepath.Join(a.GetDataDir(), constants.ProcessExeName)
	fi, err := os.Stat(exePath)
	if err != nil {
		return ""
	}
	mt := fi.ModTime()
	localVersionMu.Lock()
	if localVersionPath == exePath && localVersionValue != "" && localVersionMtime.Equal(mt) {
		v := localVersionValue
		localVersionMu.Unlock()
		return v
	}
	localVersionMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := sysexec.CommandContext(ctx, exePath, "version")
	cmd.WaitDelay = 2 * time.Second
	utils.HideCommandWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	var ver string
	if m := singboxSemverRe.FindStringSubmatch(string(out)); len(m) > 1 {
		ver = m[1]
	}
	localVersionMu.Lock()
	localVersionPath = exePath
	localVersionMtime = mt
	localVersionValue = ver
	localVersionMu.Unlock()
	return ver
}

// IsSingboxUpdateAvailable is true only when the server build is strictly newer
// than the installed one — never on an equal or older server version, so the
// launch-time auto-update never downgrades (matching the macOS client).
func (a *App) IsSingboxUpdateAvailable() bool {
	if !a.IsSingboxInstalled() {
		return false
	}
	local := a.GetSingboxLocalVersion()
	if local == "" {
		return false
	}
	remote := update.GetRemoteSingboxVersion()
	if remote == "" {
		return false
	}
	return semver.Compare(remote, local) > 0
}

// InstallSingbox downloads sing-box from the update server, reporting download
// percent to onProgress (0..100) so the UI can show a progress bar. onProgress may
// be nil.
func (a *App) InstallSingbox(onProgress func(int)) (version string, err error) {
	dataDir := a.GetDataDir()
	logFn := progressLogFn(controllerLog(dataDir, true), onProgress)
	_, err = update.DownloadAndExtract(dataDir, logFn)
	if err != nil {
		return "", err
	}
	return a.GetSingboxLocalVersion(), nil
}

func (a *App) UpdateSingbox(onProgress func(int)) (version string, err error) {
	if core.IsRunning() {
		if _, err := a.Disconnect(); err != nil {
			return "", err
		}
	}
	dataDir := a.GetDataDir()
	logFn := progressLogFn(controllerLog(dataDir, true), onProgress)
	_, err = update.DownloadAndExtract(dataDir, logFn)
	if err != nil {
		return "", err
	}
	return a.GetSingboxLocalVersion(), nil
}

// progressLogFn wraps base so the download's "PROGRESS <pct>" lines are delivered
// to onProgress as an integer percent instead of the log file; all other lines are
// logged normally.
func progressLogFn(base func(string, string), onProgress func(int)) func(string, string) {
	return func(msg, level string) {
		if strings.EqualFold(strings.TrimSpace(level), "PROGRESS") {
			if onProgress != nil {
				if pct, err := strconv.Atoi(strings.TrimSpace(msg)); err == nil {
					onProgress(pct)
				}
			}
			return
		}
		base(msg, level)
	}
}
