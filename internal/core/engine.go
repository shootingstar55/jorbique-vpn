package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"jorbiquevpn/internal/config"
	"jorbiquevpn/internal/constants"
	"jorbiquevpn/internal/jsonorder"
	"jorbiquevpn/internal/network"
	"jorbiquevpn/internal/paths"
	"jorbiquevpn/internal/singboxcfg"
	"jorbiquevpn/internal/sysexec"
	"jorbiquevpn/internal/utils"
)

const singboxPidFile = ".singbox_pid"

// Coalesce rapid tasklist checks (UI + watchdog + internal loops) without hiding
// state changes: TTL is shorter than waitForProcessRunning / Stop poll intervals.
const isRunningCacheTTL = 100 * time.Millisecond

var (
	isRunningMu       sync.Mutex // guards the cache fields below; held only briefly
	isRunningCached   bool
	isRunningCachedAt time.Time
	isRunningEpoch    uint64 // bumped on every invalidate so a slow computation can't publish stale state

	// isRunningComputeMu single-flights isRunningRaw without holding isRunningMu, so its stale-PID
	// retry sleeps (~160ms) never block cache reads or invalidateIsRunningCache.
	isRunningComputeMu sync.Mutex
)

var (
	// tunNameSeq is a monotonic per-process counter; tunNameBase varies per run from the wall clock.
	// Combined they make every nextTunName() distinct within a process (so back-to-back retries never
	// collide) while still differing from an adapter leaked by a previous run.
	tunNameSeq  uint64
	tunNameBase = uint64(time.Now().UnixNano()) % 1000000
)

// lastStartNanos is the wall-clock time of the last Start, in Unix nanoseconds;
// see StartedAt.
var lastStartNanos atomic.Int64

// StartedAt is when this process last started sing-box, or the zero time if it
// has not — a core it found already running from an earlier run counts as long
// settled.
func StartedAt() time.Time {
	if n := lastStartNanos.Load(); n != 0 {
		return time.Unix(0, n)
	}
	return time.Time{}
}

// nextTunName returns a unique sing-box TUN interface name. UnixNano()%N could repeat at coarse
// clock resolution, which would defeat the collision-avoidance the unique-name feature exists for.
func nextTunName() string {
	seq := atomic.AddUint64(&tunNameSeq, 1)
	return fmt.Sprintf("singtun%d", (tunNameBase+seq)%1000000)
}

func invalidateIsRunningCache() {
	isRunningMu.Lock()
	isRunningCachedAt = time.Time{}
	isRunningEpoch++
	isRunningMu.Unlock()
}

func singboxPidPath(dataDir string) string {
	return filepath.Join(dataDir, singboxPidFile)
}

// lastLines returns the trailing n non-empty lines of s, collapsed onto one line,
// so a multi-line sing-box crash dump stays readable inside a single log entry.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	out := make([]string, 0, n)
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		t := strings.TrimSpace(lines[i])
		if t != "" {
			out = append([]string{t}, out...)
		}
	}
	return strings.Join(out, " | ")
}

// singboxLogTail returns the trailing lines of sing-box's own log file (log.output in the
// runtime config). sing-box logs TUN/Wintun creation failures there, not to stdout, so without
// this the real reason a connect failed ("TUN interface not ready") never reaches controller.log.
func singboxLogTail(fullConfigPath, dataDir string, n int) string {
	data, err := os.ReadFile(fullConfigPath)
	if err != nil {
		return ""
	}
	var cfg struct {
		Log struct {
			Output string `json:"output"`
		} `json:"log"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return ""
	}
	out := strings.TrimSpace(cfg.Log.Output)
	if out == "" {
		return ""
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(dataDir, filepath.FromSlash(out))
	}
	logData, err := os.ReadFile(out)
	if err != nil {
		return ""
	}
	return lastLines(string(logData), n)
}

func readSingboxPID(dataDir string) (int, bool) {
	data, err := os.ReadFile(singboxPidPath(dataDir))
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

func writeSingboxPIDFile(dataDir string, pid int) {
	_ = os.WriteFile(singboxPidPath(dataDir), []byte(fmt.Sprintf("%d", pid)), 0644)
}

func removeSingboxPIDFile(dataDir string) {
	_ = os.Remove(singboxPidPath(dataDir))
}

// singboxProcessAnywhere and tunInterfaceUp are the two halves of
// untrackedTunnelRunning; tests swap them.
var (
	singboxProcessAnywhere = singboxProcessExistsByImageName
	tunInterfaceUp         = func() bool {
		_, _, ok := network.FindTunInterface("", constants.TunIPRange)
		return ok
	}
)

// untrackedTunnelRunning is the fallback for a tunnel whose PID file was lost: some
// sing-box.exe runs and our TUN is up. A sing-box.exe alone is not a tunnel — the
// `sing-box version` check right after a download is one too, and counting it put a
// false "Подключено" on screen for the second or two it lived.
func untrackedTunnelRunning() bool {
	return singboxProcessAnywhere() && tunInterfaceUp()
}

func isRunningRaw() bool {
	dataDir := paths.DataDir()
	pid, ok := readSingboxPID(dataDir)
	if !ok {
		return untrackedTunnelRunning()
	}
	if pidProcessIsOurSingbox(pid) {
		return true
	}
	// Transient failure or stale PID — confirm before dropping session tracking.
	for i := 0; i < 2; i++ {
		time.Sleep(80 * time.Millisecond)
		if pidProcessIsOurSingbox(pid) {
			return true
		}
	}
	if snapPID, alive := findSingboxPIDBySnapshot(); alive && pidProcessIsOurSingbox(snapPID) {
		writeSingboxPIDFile(dataDir, snapPID)
		return true
	}
	removeSingboxPIDFile(dataDir)
	return untrackedTunnelRunning()
}

func waitForTunInterface(tunConfigPath string) bool {
	// Prefer to find singtun before touching routes; cap wait so total connect stays under ~10s.
	maxWait := time.Duration(constants.ConnectTunWaitMaxSec) * time.Second
	deadline := time.Now().Add(maxWait)
	tunName := ""
	if tunConfigPath != "" {
		tunName = network.GetTunInterfaceNameFromConfig(tunConfigPath)
	}
	for time.Now().Before(deadline) {
		if !IsRunning() {
			return false
		}
		if _, _, ok := network.FindTunInterface(tunName, constants.TunIPRange); ok {
			return true
		}
		time.Sleep(75 * time.Millisecond) // native detection is ~free, so poll tightly
	}
	if !IsRunning() {
		return false
	}
	_, _, ok := network.FindTunInterface(tunName, constants.TunIPRange)
	return ok
}

func waitForPID(pid int, maxWait time.Duration) bool {
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		if pidProcessIsOurSingbox(pid) {
			return true
		}
		time.Sleep(150 * time.Millisecond)
	}
	return pidProcessIsOurSingbox(pid)
}

// IsRunning checks if sing-box process is running
func IsRunning() bool {
	if v, ok := cachedIsRunning(); ok {
		return v
	}

	// Single-flight the (possibly slow) computation without holding isRunningMu — isRunningRaw can
	// sleep ~160ms on the stale-PID retry path, and holding the cache lock across that would stall
	// the tray ticker / watchdog and any Stop/Start calling invalidateIsRunningCache.
	isRunningComputeMu.Lock()
	defer isRunningComputeMu.Unlock()

	// A peer may have refreshed the cache while we waited for the compute lock.
	if v, ok := cachedIsRunning(); ok {
		return v
	}

	isRunningMu.Lock()
	epoch := isRunningEpoch
	isRunningMu.Unlock()

	v := isRunningRaw()

	isRunningMu.Lock()
	// Only publish when no invalidation (e.g. a concurrent Stop/Start) happened during the compute,
	// so a slow reading never overwrites a fresher invalidation.
	if isRunningEpoch == epoch {
		isRunningCached = v
		isRunningCachedAt = time.Now()
	}
	isRunningMu.Unlock()
	return v
}

// cachedIsRunning returns the cached running state when still within the TTL.
func cachedIsRunning() (value, ok bool) {
	isRunningMu.Lock()
	defer isRunningMu.Unlock()
	if !isRunningCachedAt.IsZero() && time.Since(isRunningCachedAt) < isRunningCacheTTL {
		return isRunningCached, true
	}
	return false, false
}

// Stop terminates the sing-box instance started by this app (or falls back to killing by image name).
func Stop(logFn func(string, string)) {
	invalidateIsRunningCache()
	dataDir := paths.DataDir()
	pid, hadFile := readSingboxPID(dataDir)
	if hadFile && pid > 0 {
		_ = utils.TerminateByPID(pid)
	} else {
		_ = utils.TerminateByName(constants.ProcessExeName)
	}
	invalidateIsRunningCache()
	for i := 0; i < 15; i++ { // ~3s max wait (15 * 200ms)
		still := false
		if hadFile && pid > 0 {
			still = pidProcessIsOurSingbox(pid)
		} else {
			still = singboxProcessExistsByImageName()
		}
		if !still {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	removeSingboxPIDFile(dataDir)
	invalidateIsRunningCache()
	if logFn != nil {
		logFn("Stopped sing-box process(es)", "INFO")
	}
}

// assignFreshTunName rewrites the TUN inbound's interface_name in the runtime config to a unique
// value. This is the root-cause fix for the Win10 "configure tun interface: Cannot create a file
// when that file already exists" failure: a leftover adapter from a force-killed prior run (or a
// killed retry attempt) carries the OLD name, so a fresh name never collides with it. Routing and
// TUN detection are entirely IP-based (172.31.255.*), so the interface name is free to change.
func assignFreshTunName(configPath string) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return
	}
	// Ordered so the rewrite does not reshuffle a file the builder just wrote in
	// the profile's own order; numbers keep the digits they arrived with.
	cfg := jsonorder.New()
	if jsonorder.Unmarshal(data, cfg) != nil {
		return
	}
	raw, ok := cfg.Get("inbounds")
	if !ok {
		return
	}
	inbounds, ok := raw.([]any)
	if !ok {
		return
	}
	name := nextTunName()
	changed := false
	for _, ib := range inbounds {
		m, ok := ib.(*jsonorder.Map)
		if !ok {
			continue
		}
		if m.GetString("type") == "tun" {
			m.Set("interface_name", name)
			changed = true
		}
	}
	if !changed {
		return
	}
	// The same writer the builder uses, so the file keeps the shape it was
	// written in rather than coming back minified and reordered.
	if out, err := config.MarshalSingBoxConfigOrdered(cfg); err == nil {
		_ = os.WriteFile(configPath, out, 0644)
	}
}

// syncBuffer is an io.Writer whose contents can be read while it is still being written. exec
// spawns copier goroutines that write a process's stdout/stderr into runCmd.Stdout for as long as
// the process lives, so reading a plain *bytes.Buffer concurrently is a data race.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// launchSingboxOnce starts sing-box once and waits for its TUN to come up. It reports whether the
// process is running, whether the TUN was confirmed (IP assigned), and returns the captured
// stdout/stderr for diagnostics. The caller decides whether to retry or proceed.
func launchSingboxOnce(singboxExe, fullConfigPath, dataDir string, cfg config.Config, logFn func(string, string)) (running, tunReady bool, outBuf *syncBuffer) {
	outBuf = &syncBuffer{}
	runCmd := sysexec.Command(singboxExe, "run", "-c", fullConfigPath)
	runCmd.Env = singboxcfg.EngineEnvironment(os.Environ())
	runCmd.Dir = dataDir
	runCmd.Stdout = outBuf
	runCmd.Stderr = outBuf
	utils.HideCommandWindow(runCmd)
	if err := runCmd.Start(); err != nil {
		if logFn != nil {
			logFn("Failed to start sing-box: "+err.Error(), "ERROR")
		}
		return false, false, outBuf
	}
	singboxPID := runCmd.Process.Pid

	maxWait := time.Duration(cfg.MaxStartupChecks*cfg.StartupCheckDelay) * time.Second
	if maxWait <= 0 {
		maxWait = 3 * time.Second
	}
	if waitForPID(singboxPID, maxWait) {
		writeSingboxPIDFile(dataDir, singboxPID)
		invalidateIsRunningCache()
		if logFn != nil {
			logFn("Sing-box process ready", "INFO")
		}
	}

	// Always reap the started process in the background (exactly one Wait per successful Start).
	// This releases the exec stdout/stderr copier goroutines — the previous !started path called
	// Process.Release() but never Wait(), leaking those goroutines/pipes — and surfaces the exit
	// reason (AV/Defender kill, panic) in the log instead of looking like a random auto-disconnect.
	// outBuf is a syncBuffer, so this reaper reading it never races a caller of outBuf.String().
	go reapSingbox(runCmd, outBuf, logFn)

	if !IsRunning() {
		return false, false, outBuf
	}
	if logFn != nil {
		logFn("Waiting for TUN interface...", "INFO")
	}
	tunReady = waitForTunInterface(fullConfigPath)
	return IsRunning(), tunReady, outBuf
}

func reapSingbox(runCmd *exec.Cmd, outBuf *syncBuffer, logFn func(string, string)) {
	waitErr := runCmd.Wait()
	invalidateIsRunningCache()
	if logFn == nil {
		return
	}
	out := strings.TrimSpace(outBuf.String())
	if out != "" {
		out = ": " + lastLines(out, 12)
	}
	if waitErr != nil {
		logFn("sing-box process exited ("+waitErr.Error()+")"+out, "WARNING")
	} else {
		logFn("sing-box process exited cleanly"+out, "INFO")
	}
}

// Start launches sing-box with config. Returns (success, errorMessage).
func Start(cfgPath, dataDir, singboxExe string, cfg config.Config,
	logFn func(string, string)) (bool, string) {

	invalidateIsRunningCache()
	removeSingboxPIDFile(dataDir)
	startTime := time.Now()
	lastStartNanos.Store(startTime.UnixNano())
	fullConfigPath := cfgPath
	if !filepath.IsAbs(cfgPath) {
		fullConfigPath = filepath.Join(dataDir, cfgPath)
	}
	if _, err := os.Stat(fullConfigPath); err != nil {
		if logFn != nil {
			logFn("Config file not found: "+fullConfigPath, "ERROR")
		}
		return false, "Config file not found: " + fullConfigPath
	}

	logsDir := filepath.Join(dataDir, "logs")
	_ = os.MkdirAll(logsDir, 0755)
	go network.ClearDnsCache() // flushing the DNS cache need not block the connect path
	if logFn != nil {
		logFn("Config path: "+fullConfigPath, "INFO")
	}

	// Give this run a unique TUN interface name so a leftover adapter can never collide.
	assignFreshTunName(fullConfigPath)

	if errMsg := checkSingboxConfig(singboxExe, fullConfigPath, logFn); errMsg != "" {
		return false, errMsg
	}

	running, tunReady, outBuf := launchSingboxWithRetry(singboxExe, fullConfigPath, dataDir, cfg, logFn)

	if !running {
		errMsg := singboxStartupError(outBuf, fullConfigPath, dataDir)
		if logFn != nil {
			logFn(errMsg, "ERROR")
		}
		removeSingboxPIDFile(dataDir)
		return false, errMsg
	}

	// Marks a session in progress; a disconnect removes it.
	_ = os.WriteFile(filepath.Join(dataDir, ".current_profile"), nil, 0644)

	logTunReadiness(tunReady, fullConfigPath, dataDir, logFn)

	// One catch-all NRPT rule puts every DNS query in the tunnel; sing-box decides
	// the rest.
	network.SetNrptDnsCapture(cfg.GatewayIP, logFn)

	if !IsRunning() {
		network.RemoveNrptDnsCapture(logFn)
		removeSingboxPIDFile(dataDir)
		msg := "sing-box exited during startup"
		if logFn != nil {
			logFn(msg, "ERROR")
		}
		return false, msg
	}

	// Persist state for tray stop
	network.SaveState(dataDir)

	elapsed := time.Since(startTime).Round(time.Millisecond)
	if logFn != nil {
		pidInfo := ""
		if pid, ok := readSingboxPID(dataDir); ok {
			pidInfo = "PID: " + strconv.Itoa(pid) + ", "
		}
		logFn("Connected ("+pidInfo+"total: "+elapsed.String()+")", "SUCCESS")
	}
	return true, ""
}

func checkSingboxConfig(singboxExe, fullConfigPath string, logFn func(string, string)) string {
	// Config check
	if logFn != nil {
		logFn("Checking configuration...", "INFO")
	}
	checkCmd := sysexec.Command(singboxExe, "check", "-c", fullConfigPath)
	utils.HideCommandWindow(checkCmd)
	if out, err := checkCmd.CombinedOutput(); err != nil {
		errStr := strings.TrimSpace(string(out))
		if logFn != nil {
			logFn("Config check failed: "+errStr, "ERROR")
		}
		return "Config check failed: " + errStr
	}
	if logFn != nil {
		logFn("Config valid, starting sing-box...", "INFO")
	}

	return ""
}

func launchSingboxWithRetry(singboxExe, fullConfigPath, dataDir string, cfg config.Config, logFn func(string, string)) (bool, bool, *syncBuffer) {
	// Launch sing-box, retrying once if the TUN never comes up (e.g. a transient driver hiccup).
	// Each attempt gets a fresh interface name, so a retry never collides with the adapter the
	// previous attempt may have just created and leaked when it was killed.
	const maxAttempts = 2
	var (
		outBuf            *syncBuffer
		running, tunReady bool
	)
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			if logFn != nil {
				logFn(fmt.Sprintf("TUN did not come up; cleaning and retrying sing-box (attempt %d/%d)", attempt, maxAttempts), "WARNING")
			}
			Stop(logFn)
			network.PrepareSingtunForConnect(network.GetTunInterfaceNameFromConfig(fullConfigPath), logFn)
			assignFreshTunName(fullConfigPath)
		}
		running, tunReady, outBuf = launchSingboxOnce(singboxExe, fullConfigPath, dataDir, cfg, logFn)
		if running && tunReady {
			break
		}
		if running && attempt == maxAttempts {
			break // last attempt: sing-box is alive, carry on even if TUN was not confirmed
		}
	}

	return running, tunReady, outBuf
}

func singboxStartupError(outBuf *syncBuffer, fullConfigPath, dataDir string) string {
	errMsg := "Sing-box failed to start or exited immediately"
	if outBuf != nil {
		if out := strings.TrimSpace(outBuf.String()); out != "" {
			errMsg = out
		}
	}
	if errMsg == "Sing-box failed to start or exited immediately" {
		if tail := singboxLogTail(fullConfigPath, dataDir, 12); tail != "" {
			errMsg += ": " + tail
		}
	}
	return errMsg
}

func logTunReadiness(tunReady bool, fullConfigPath, dataDir string, logFn func(string, string)) {
	if !tunReady && logFn != nil {
		// sing-box is alive but adapter detection lagged: proceed anyway — sing-box
		// brings the interface and its routes up on its own. (Hard-failing here
		// used to break every connect.)
		logFn("TUN interface not confirmed within wait; continuing", "WARNING")
		if tail := singboxLogTail(fullConfigPath, dataDir, 8); tail != "" {
			logFn("sing-box log: "+tail, "INFO")
		}
	}
}
