package launchd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"jorbiquevpn/internal/macos/ansiutil"
	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/macos/fsatomic"
	"jorbiquevpn/internal/macos/logger"
	"jorbiquevpn/internal/macos/procutil"
)

const systemDomainPrefix = "system/"

const DefaultLabel = "one.jorbique.vpn"

func PlistPath(label string) string {
	if label == "" {
		label = DefaultLabel
	}
	return "/Library/LaunchDaemons/" + label + ".plist"
}

// daemonLogsDir is where launchd writes the daemon's stdout and stderr. It used
// to be a "logs" directory next to the binary, which was fine while that
// directory held every log; it does not any more. The install directory is for
// binaries, and the daemon was the one writer still dropping files into it.
func daemonLogsDir(executablePath string) string {
	if dir := strings.TrimSpace(core.NewPaths().LogDir); dir != "" {
		return dir
	}
	return filepath.Join(filepath.Dir(executablePath), "logs")
}

func plistContent(executablePath string, label string) string {
	logsDir := daemonLogsDir(executablePath)
	outPath := filepath.Join(logsDir, "daemon.out.log")
	errPath := filepath.Join(logsDir, "daemon.err.log")
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
  <dict>
    <key>Label</key>
    <string>` + label + `</string>

    <key>ProgramArguments</key>
    <array>
      <string>` + executablePath + `</string>
      <string>--daemon</string>
    </array>

    <key>RunAtLoad</key>
    <true/>

    <key>KeepAlive</key>
    <true/>

    <key>SessionCreate</key>
    <true/>

    <key>StandardOutPath</key>
    <string>` + outPath + `</string>
    <key>StandardErrorPath</key>
    <string>` + errPath + `</string>
  </dict>
</plist>
`
}

// RefreshDaemonIfStale rewrites an installed LaunchDaemon that still starts a
// binary somewhere else. launchd would go on starting the old path at every
// boot, so the daemon half of the client would stay on the version the upgrade
// replaced. No plist means autostart was never installed and there is nothing
// to fix.
// daemonPlistIsStale reports whether the installed plist starts a binary other
// than this one. The path is matched between its element tags rather than
// anywhere in the file: /usr/local/libexec/jorbique-vpn/JorbiqueVPN appears
// inside every log path in the same plist, and a bare substring test would call
// a plist current on the strength of its StandardOutPath.
//
// Split out of RefreshDaemonIfStale because that function reads a fixed system
// path and does nothing at all unless it is root — a rule no test can satisfy,
// which left the decision it exists to make untested.
func daemonPlistIsStale(plist, executablePath string) bool {
	return !strings.Contains(plist, "<string>"+executablePath+"</string>")
}

// jobIsDisabled reports whether the label appears as disabled in the output of
// `launchctl print-disabled`. The wording has changed across releases — older
// ones print `=> true`, current ones `=> disabled` — so both are read, and a
// line that says neither is taken as enabled.
func jobIsDisabled(printDisabledOutput, label string) bool {
	for _, line := range strings.Split(printDisabledOutput, "\n") {
		if !strings.Contains(line, `"`+label+`"`) {
			continue
		}
		return strings.Contains(line, "=> true") || strings.Contains(line, "=> disabled")
	}
	return false
}

// daemonIsDisabled asks launchd whether an administrator turned this job off.
func daemonIsDisabled(label string) bool {
	st, out := procutil.RunAndCapture(launchctlExecutable, "print-disabled", "system")
	if st != 0 {
		return false
	}
	return jobIsDisabled(out, label)
}

func RefreshDaemonIfStale(executablePath, label string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	if label == "" {
		label = DefaultLabel
	}
	data, err := os.ReadFile(PlistPath(label))
	if err != nil {
		return nil
	}
	if !daemonPlistIsStale(string(data), executablePath) {
		return nil
	}
	// Install ends in `launchctl enable`, which is right when someone asked for
	// the daemon and wrong here: this runs by itself, on every root start, and
	// an upgrade is not permission to undo `launchctl disable`. A machine taken
	// out of service on purpose would come back up serving, and nothing would
	// have said so.
	if daemonIsDisabled(label) {
		return fmt.Errorf("the plist still names another binary, but %s is disabled — "+
			"leaving it that way (launchctl enable system/%s to undo)", label, label)
	}
	return Install(executablePath, label, logger.New(filepath.Join(daemonLogsDir(executablePath), "app.log")))
}

func Install(executablePath string, label string, log *logger.Logger) error {
	if os.Geteuid() != 0 {
		return core.Permission("install-autostart must be run as root (sudo).")
	}
	if label == "" {
		label = DefaultLabel
	}
	_ = os.MkdirAll(daemonLogsDir(executablePath), 0o755)

	// Written beside the target and renamed over it: launchd reads a plist as
	// it appears, and a half-written one is a plist it may try to load. This
	// runs as root, so writing the file is a write, not a call to
	// /usr/bin/install.
	plistPath := PlistPath(label)
	if err := fsatomic.WriteFile(plistPath, []byte(plistContent(executablePath, label)), 0o644); err != nil {
		log.Warn("could not write " + plistPath + ": " + err.Error())
		return err
	}
	if st, _ := procutil.RunProcess(launchctlExecutable, []string{"enable", systemDomainPrefix + label}, false); st != 0 {
		log.Warn(fmt.Sprintf("launchctl enable exit=%d", st))
	}

	if Loaded(label) {
		return reload(plistPath, label, log)
	}

	if err := bootstrapWithRetry(plistPath, label, log); err != nil {
		return err
	}
	log.Log(ansiutil.Green("✓ Installed LaunchDaemon: " + plistPath))
	return nil
}

// reload replaces a running job with the plist just written.
//
// `kickstart -k` alone does not do this. It restarts the job from the
// definition launchd already holds in memory, so a plist naming a different
// binary is ignored until the machine reboots — and a different binary is the
// only thing RefreshDaemonIfStale ever fires on (daemonPlistIsStale compares
// the path). The one mechanism meant to fix a stale daemon could not, because
// the case that triggers it is the case kickstart cannot carry.
//
// Booting the job out first is what picks up the new plist, and it is also what
// once ended an upgrade with no daemon at all: bootout returns before the job
// is gone and the bootstrap behind it fails with EIO. That is what waiting for
// it to actually go, and retrying the bootstrap, are for.
//
// If the replacement still does not take, the job is kickstarted instead. The
// binary is at least restarted then, and the machine keeps a daemon — being one
// upgrade behind is recoverable, having none is not.
func reload(plistPath, label string, log *logger.Logger) error {
	if st, _ := procutil.RunProcess(launchctlExecutable, []string{"bootout", systemDomainPrefix + label}, false); st != 0 {
		log.Warn(fmt.Sprintf("launchctl bootout exit=%d", st))
	}
	if !waitUnloaded(label) {
		log.Warn("the old daemon is still loaded; restarting it in place instead of replacing it")
		return kickstart(plistPath, label, log)
	}
	if err := bootstrapWithRetry(plistPath, label, log); err != nil {
		log.Warn("could not bootstrap the new daemon (" + err.Error() + "); restarting whatever is there")
		return kickstart(plistPath, label, log)
	}
	log.Log(ansiutil.Green("✓ LaunchDaemon replaced: " + plistPath))
	return nil
}

// kickstart restarts the loaded job without reloading its definition. The
// fallback, never the first choice — see reload.
func kickstart(plistPath, label string, log *logger.Logger) error {
	if st, _ := procutil.RunProcess(launchctlExecutable, []string{"kickstart", "-k", systemDomainPrefix + label}, false); st != 0 {
		log.Warn(fmt.Sprintf("launchctl kickstart exit=%d", st))
	}
	if !waitLoaded(label) {
		return core.StartFailed("the daemon did not come back after a restart")
	}
	log.Log(ansiutil.Green("✓ LaunchDaemon restarted: " + plistPath))
	return nil
}

// Loaded reports whether launchd has the job in the system domain.
func Loaded(label string) bool {
	if label == "" {
		label = DefaultLabel
	}
	st, _ := procutil.RunAndCapture(launchctlExecutable, "print", systemDomainPrefix+label)
	return st == 0
}

// waitLoaded gives launchd a moment to finish. Both bootstrap and kickstart
// return before the job is fully up, and a caller that reports success on a
// daemon that never started is the reason this exists.
func waitLoaded(label string) bool {
	for i := 0; i < 20; i++ {
		if Loaded(label) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// waitUnloaded waits for a booted-out job to actually be gone. bootout returns
// before launchd has finished with it, and bootstrapping over one that is still
// on its way out is the EIO that bootstrapWithRetry exists to survive — better
// not to race it in the first place.
func waitUnloaded(label string) bool {
	for i := 0; i < 30; i++ {
		if !Loaded(label) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// bootstrapWithRetry loads the job, retrying while launchd still has the old
// one on its way out — that is the EIO ("Input/output error") case.
//
// The budget matters more than it looks: postinstall fails the whole package
// install when this gives up, so it has to outlast a slow machine rather than
// only a quick hiccup. Backing off from 300ms to just under 5s per attempt
// spends around 15s before declaring the daemon unloadable, against the ~1.5s
// it used to allow.
func bootstrapWithRetry(plistPath, label string, log *logger.Logger) error {
	delay := 300 * time.Millisecond
	for attempt := 0; attempt < 8; attempt++ {
		st, _ := procutil.RunProcess(launchctlExecutable, []string{"bootstrap", "system", plistPath}, false)
		if st == 0 || Loaded(label) {
			if waitLoaded(label) {
				return nil
			}
		}
		if attempt > 0 {
			log.Warn(fmt.Sprintf("launchctl bootstrap attempt %d failed (exit=%d); retrying in %s", attempt+1, st, delay))
		}
		time.Sleep(delay)
		if delay < 4*time.Second {
			delay *= 2
		}
	}
	return core.StartFailed("launchctl bootstrap failed: the daemon is not loaded")
}
