package ui

import (
	"os"
	"slices"
	"strings"

	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/macos/singbox"
)

// AvailableVPN identifies a configured sing-box connection.
type AvailableVPN struct {
	Name string
	Type string
}

// The sing-box profile is downloaded at startup, so it need not exist yet.
func availableVPNs(cfg *core.AppConfig, paths *core.Paths) []AvailableVPN {
	out := make([]AvailableVPN, 0, len(cfg.VPNs))
	for _, v := range cfg.VPNs {
		if v.Type == core.VPNSingbox && v.File != "" && v.File != paths.CfgDir {
			out = append(out, AvailableVPN{Name: v.Name, Type: string(v.Type)})
		}
	}
	return out
}

// startedVPNs returns the names listed in the started-list file (one per line,
// blanks skipped): the configured started_list_path when set, otherwise
// core.Paths.StartedListURL.
func startedVPNs(cfg *core.AppConfig, paths *core.Paths) []string {
	startedPath := paths.StartedListURL()
	if cfg.StartedListPath != nil && *cfg.StartedListPath != "" {
		startedPath = *cfg.StartedListPath
	}
	data, err := os.ReadFile(startedPath)
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		name := strings.TrimSpace(line)
		if name != "" && slices.ContainsFunc(cfg.VPNs, func(vpn core.VPNItem) bool {
			return vpn.Type == core.VPNSingbox && vpn.Name == name
		}) {
			out = append(out, name)
		}
	}
	return out
}

// singboxUUID returns the trimmed sing-box UUID, or "" when unset. Ports
// uiLoadSingboxUUID.
func singboxUUID(cfg *core.AppConfig) string {
	return strings.TrimSpace(cfg.Singbox.UUID)
}

// singboxNaiveMode reports whether the client connects in the NaiveProxy mode.
func singboxNaiveMode(cfg *core.AppConfig) bool {
	return cfg.Singbox.NaiveMode
}

// singboxEnabledOutbounds returns the protocols that are actually on (never
// nil): what the profile offers, minus what the user refused, resolved by the
// same function the config builder uses.
//
// It used to hand back the stored positive list and answer empty for a
// configuration nobody had chosen yet, which left the callers to read empty as
// "everything is on" — wrong once every protocol could be switched off.
//
// available is what the profile offers, read once by the caller so the panel
// and what it is handed describe the same profile.
func singboxEnabledOutbounds(cfg *core.AppConfig, available []string) []string {
	if len(available) == 0 {
		// No profile downloaded yet: there is nothing to resolve a refusal
		// against, so the stored answer stands as it is — and the packaged list
		// nobody has chosen still reads as no opinion.
		if cfg.Singbox.EnabledOutbounds == nil ||
			singbox.IsUnchosenShippedDefault(cfg.Singbox.EnabledOutbounds, cfg.Singbox.DisabledOutbounds) {
			return []string{}
		}
		return cfg.Singbox.EnabledOutbounds
	}
	out := singbox.EnabledProxyTags(available, cfg.Singbox.EnabledOutbounds, cfg.Singbox.DisabledOutbounds)
	if out == nil {
		return []string{}
	}
	return out
}

// reconcileSingboxChoice carries the panel's ticks, made against the list it
// showed, over to what the profile offers when they are applied: a profile
// refresh can land while the panel is open, and set-singbox-config refuses a
// tag the profile no longer has. A tag the profile dropped goes. One the panel
// did not show gets what stored, the saved choice read against current, says
// for it: on unless the user refused it. A panel that showed nothing therefore
// sends the stored choice; its empty line would switch every protocol off.
// With no profile now, or nothing left, the ticks go as they are, and a tick
// the profile dropped makes set-singbox-config name what it offers instead.
func reconcileSingboxChoice(shown, ticked, current, stored []string) []string {
	if len(current) == 0 {
		return ticked
	}
	// Case-insensitive, like set-singbox-config and EnabledProxyTags.
	has := func(list []string, tag string) bool {
		return slices.ContainsFunc(list, func(s string) bool { return strings.EqualFold(s, tag) })
	}
	var out []string
	for _, tag := range current {
		on := has(stored, tag)
		if has(shown, tag) {
			on = has(ticked, tag)
		}
		if on {
			out = append(out, tag)
		}
	}
	if len(out) == 0 {
		return ticked
	}
	return out
}

// hasSingboxTypeRunning reports whether any running VPN is of sing-box type.
// Ports uiHasSingboxTypeRunning.
func hasSingboxTypeRunning(running []string, available []AvailableVPN) bool {
	singbox := make(map[string]struct{}, len(available))
	for _, v := range available {
		if v.Type == string(core.VPNSingbox) {
			singbox[v.Name] = struct{}{}
		}
	}
	for _, name := range running {
		if _, ok := singbox[name]; ok {
			return true
		}
	}
	return false
}

// singboxRuntimeConfigPath returns the downloaded sing-box profile used by the API client.
func singboxRuntimeConfigPath(cfg *core.AppConfig) string {
	if cfg == nil {
		return ""
	}
	for _, vpn := range cfg.VPNs {
		if vpn.Type == core.VPNSingbox && strings.TrimSpace(vpn.File) != "" {
			return vpn.File
		}
	}
	return ""
}

// singboxAvailableOutbounds lists the protocols the downloaded profile offers.
func singboxAvailableOutbounds(cfg *core.AppConfig) []string {
	for _, v := range cfg.VPNs {
		if v.Type == core.VPNSingbox && strings.TrimSpace(v.File) != "" {
			return singbox.AvailableProxyTags(v.File)
		}
	}
	return nil
}
