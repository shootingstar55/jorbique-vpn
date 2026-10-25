package vpnmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"jorbiquevpn/internal/macos/ansiutil"
	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/macos/routerules"
	"jorbiquevpn/internal/sysexec"
	"jorbiquevpn/internal/update"
)

var singboxSemverRe = regexp.MustCompile(`(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?)`)

// SingboxVersions reports the locally installed sing-box version and the version
// advertised by the single update server.
type SingboxVersions struct {
	Local  string `json:"local"`
	Remote string `json:"remote"`
}

// localSingboxVersion runs the resolved sing-box binary and parses its semver, or "" if unavailable.
// singboxCallTimeout bounds the short, synchronous calls to the sing-box
// binary. They are a version string and a config rewrite; neither has any
// business taking longer, and both sit on paths the user is waiting on. A var
// so a test can shrink it rather than wait it out.
var singboxCallTimeout = 20 * time.Second

// singboxWaitDelay is how long Wait may go on reading output after the deadline
// has already killed the process.
//
// A context on its own does not bound these calls. CommandContext kills the
// process it started, but CombinedOutput returns only when the output pipe is
// closed, and any child that process left behind still holds it — so a sing-box
// that forks and hangs would keep the caller waiting for as long as the child
// lived, deadline or no deadline. WaitDelay is what closes the pipe and lets
// Wait return.
const singboxWaitDelay = 2 * time.Second

// runBounded runs a sing-box subcommand under ctx and actually returns when ctx
// expires. The path is the app-managed binary and the arguments are fixed by
// the caller, never built from user input.
func runBounded(ctx context.Context, exe string, args ...string) ([]byte, error) {
	cmd := sysexec.CommandContext(ctx, exe, args...)
	cmd.WaitDelay = singboxWaitDelay
	return cmd.CombinedOutput()
}

func (m *Manager) localSingboxVersion() string {
	out := m.singboxVersionOutput()
	if mt := singboxSemverRe.FindStringSubmatch(out); len(mt) > 1 {
		return mt[1]
	}
	return ""
}

// singboxVersionOutput is what the installed sing-box says about itself, or ""
// when there is no binary to ask or it would not answer.
func (m *Manager) singboxVersionOutput() string {
	exe := strings.TrimSpace(m.singboxExe)
	// A daemon started before the first download has no resolved binary yet.
	if m.Paths != nil && m.Paths.BinDir != "" {
		installed := filepath.Join(m.Paths.BinDir, "sing-box")
		if _, err := os.Stat(installed); err == nil {
			exe = installed
		}
	}
	if exe == "" {
		return ""
	}
	// The installed sing-box is asked for its own version. It stays an external
	// process on purpose: the client downloads and updates that binary, so
	// linking sing-box as a library would answer for the version we built
	// against rather than the one that will run the tunnel. The path comes from
	// the install layout, the argument is a constant, and the call is bounded —
	// a wedged binary must not hold the launch path.
	ctx, cancel := context.WithTimeout(context.Background(), singboxCallTimeout)
	defer cancel()
	out, err := runBounded(ctx, exe, "version")
	if err != nil {
		return ""
	}
	return string(out)
}

// naiveBuildTag is the build tag a sing-box needs for the naive outbound.
const naiveBuildTag = "with_naive_outbound"

// hasBuildTag reads the "Tags:" line of `sing-box version`.
func hasBuildTag(versionOutput, tag string) bool {
	for _, line := range strings.Split(versionOutput, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "Tags:")
		if !ok {
			continue
		}
		for _, t := range strings.Split(rest, ",") {
			if strings.TrimSpace(t) == tag {
				return true
			}
		}
	}
	return false
}

// ensureNaiveSingbox makes sure the installed sing-box can run the naive
// outbound before the NaiveProxy mode starts it.
//
// Channel builds before the server switched its macOS binaries to the official
// release (which carries naive) were built without it, and an update only
// happens when the version changes — so a machine can hold a binary of the
// current version that cannot run naive. One download from the channel fixes
// that; if the channel's build has no naive either, the start is refused with
// a reason instead of a sing-box that exits on its config.
func (m *Manager) ensureNaiveSingbox() error {
	return m.ensureSingboxBuildTag(naiveBuildTag, "naive outbound")
}

func (m *Manager) ensureSingboxBuildTag(tag, feature string) error {
	if hasBuildTag(m.singboxVersionOutput(), tag) {
		return nil
	}
	m.Logger.Log("The installed sing-box has no " + feature + "; downloading the channel's build")
	if m.Paths != nil && m.Paths.BinDir != "" {
		path, err := update.DownloadAndExtract(m.Paths.BinDir, func(msg, level string) {
			if strings.EqualFold(strings.TrimSpace(level), "ERROR") {
				m.Logger.Error(msg)
			}
		})
		if err != nil {
			m.Logger.Warn("sing-box download for " + feature + " failed: " + err.Error())
		} else if path != "" {
			m.singboxExe = path
		}
	}
	if hasBuildTag(m.singboxVersionOutput(), tag) {
		return nil
	}
	return core.StartFailed("this sing-box build has no " + feature + "; update sing-box from the Extended channel")
}

// SingboxVersionsInfo gathers the local and remote sing-box versions.
func (m *Manager) SingboxVersionsInfo() SingboxVersions {
	return SingboxVersions{
		Local:  m.localSingboxVersion(),
		Remote: update.GetRemoteSingboxVersion(),
	}
}

// PrintSingboxVersions writes the version info as a single JSON line to stdout (consumed by the menu-bar UI).
func (m *Manager) PrintSingboxVersions() error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(m.SingboxVersionsInfo())
}

// UpdateSingboxBinary replaces the sing-box binary next to the client with
// the latest from the update server (same layout as the Windows client). Any
// sing-box tunnel is stopped for the swap and started again afterwards; the
// update is checked on a schedule while the app is open, so it has to leave the
// user connected exactly as it found them.
func (m *Manager) UpdateSingboxBinary() error {
	// Only sing-box profiles hold the file being replaced.
	wasRunning := m.runningSingboxVPNs()
	if len(wasRunning) > 0 {
		m.Logger.Log("Stopping sing-box VPNs before the binary update…")
		for _, v := range wasRunning {
			m.Stop(v.Name)
		}
	}
	logFn := func(msg, level string) {
		switch strings.ToUpper(strings.TrimSpace(level)) {
		case "ERROR":
			m.Logger.Error(msg)
		case "WARN", "WARNING":
			m.Logger.Warn(msg)
		case "PROGRESS":
			// Machine-readable line parsed by the menu-bar download window.
			fmt.Println("PROGRESS " + msg)
		default:
			m.Logger.Log(msg)
		}
	}
	path, err := update.DownloadAndExtract(m.Paths.BinDir, logFn)
	if err != nil {
		m.restartAfterUpdate(wasRunning, "sing-box update failed")
		return err
	}
	if path != "" {
		m.singboxExe = path
	} else {
		m.singboxExe = filepath.Join(m.Paths.BinDir, "sing-box")
	}
	m.Logger.Log(ansiutil.Green("✓ sing-box updated → " + m.singboxExe))
	// Success restores the connection too. It used to not, which was survivable
	// while this only ran at launch with nothing up yet; on a schedule it would
	// read as the VPN dropping on its own.
	m.restartAfterUpdate(wasRunning, "sing-box updated")
	return nil
}

// runningSingboxVPNs returns the running VPNs that use the sing-box binary.
func (m *Manager) runningSingboxVPNs() []core.VPNItem {
	var out []core.VPNItem
	for _, v := range m.runningVPNs() {
		if v.Type == core.VPNSingbox {
			out = append(out, v)
		}
	}
	return out
}

// restartAfterUpdate re-launches the VPNs that were running before the binary
// swap, whichever way it ended: an aborted update must not leave the user
// offline, and a successful one must not either.
func (m *Manager) restartAfterUpdate(wasRunning []core.VPNItem, why string) {
	if len(wasRunning) == 0 {
		return
	}
	names := make([]string, 0, len(wasRunning))
	for _, v := range wasRunning {
		names = append(names, v.Name)
	}
	m.Logger.Log(why + "; restarting: " + strings.Join(names, ", "))
	if err := m.Start(names); err != nil {
		m.Logger.Error("failed to restart VPNs after the sing-box update: " + err.Error())
	}
}

// normalizeSingboxConfigs hands every sing-box config to `sing-box format -w`.
//
// The binary is the only authority on what its own version accepts. It orders
// keys the way the schema declares them, drops fields that equal their default
// (`action: "route"` on a DNS rule), and rewrites anything the release moved —
// so the file on disk matches the documentation for the sing-box actually
// installed instead of a key-order table in this repo that drifts with every
// release. Best effort: no binary, or a binary that refuses the file, leaves
// our own formatting in place and the reason in the log.
func (m *Manager) normalizeSingboxConfigs() {
	exe := strings.TrimSpace(m.singboxExe)
	if exe == "" {
		return
	}
	for _, p := range routerules.SingboxConfigPathsForRoutePatch(m.Paths, m.Cfg) {
		// Same reasoning as localSingboxVersion, and more so: the point of this
		// call is that the installed binary — not a library pinned at our build
		// time — decides what its own version accepts.
		ctx, cancel := context.WithTimeout(context.Background(), singboxCallTimeout)
		out, err := runBounded(ctx, exe, "format", "-w", "-c", p)
		cancel()
		if err != nil {
			m.Logger.Warn("sing-box format declined " + p + ": " + strings.TrimSpace(string(out)))
			continue
		}
		m.Logger.Debug("sing-box format normalized " + p)
	}
}
