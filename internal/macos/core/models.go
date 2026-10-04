package core

import (
	"strconv"
	"strings"
	"time"
)

type VPNType string

const VPNSingbox VPNType = "singbox"

type SingboxSection struct {
	UUID string `json:"uuid"`
	// EnabledOutbounds is the older way of recording the choice: the tags that
	// are on. It cannot tell "the user turned this off" from "this did not
	// exist when they chose", so an outbound added on the server afterwards
	// stayed off for anyone who had ever opened the panel.
	EnabledOutbounds []string `json:"enabled_outbounds"`
	// DisabledOutbounds is what the user said no to. Anything the profile
	// offers and this does not name is on, which is the same rule the Windows
	// client has always used and the one that lets the server add a protocol.
	DisabledOutbounds []string `json:"disabled_outbounds"`
	SelectedNode      string   `json:"selected_node,omitempty"`
	// NaiveMode connects through the profile's naive outbound alone instead of
	// its other protocols. A mode of its own, not one more toggle: naive is the
	// heaviest protocol, and in the automatic selector it would run beside all
	// the others. The toggles above answer for the protocols only.
	NaiveMode bool `json:"naive_mode,omitempty"`
}

type Connectivity struct {
	MaxAttempts int    `json:"max_attempts"`
	RetryDelay  int    `json:"retry_delay"`
	Timeout     int    `json:"timeout"`
	DefaultHost string `json:"default_host"`
}

type QuietHoursSection struct {
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

type AutostartSection struct {
	CooldownSeconds           *int               `json:"cooldown_seconds,omitempty"`
	WiFiWaitTimeoutSeconds    *int               `json:"wifi_wait_timeout_seconds,omitempty"`
	NetworkChangeDelaySeconds *float64           `json:"network_change_delay_seconds,omitempty"`
	WakeDelaySeconds          *float64           `json:"wake_delay_seconds,omitempty"`
	FallbackTimeoutSeconds    *float64           `json:"fallback_timeout_seconds,omitempty"`
	HeartbeatIntervalSeconds  *int               `json:"heartbeat_interval_seconds,omitempty"`
	SSIDPollIntervalSeconds   *int               `json:"ssid_poll_interval_seconds,omitempty"`
	AutostartExclude          []string           `json:"autostart_exclude,omitempty"`
	QuietHours                *QuietHoursSection `json:"quiet_hours,omitempty"`
}

type PathsSection struct {
	RunDir              string `json:"run_dir,omitempty"`
	RunDirUser          string `json:"run_dir_user,omitempty"`
	StartedListFilename string `json:"started_list_filename,omitempty"`
	SingboxFallbackFile string `json:"singbox_fallback_file,omitempty"`
	LogDir              string `json:"log_dir,omitempty"`
	CfgDir              string `json:"cfg_dir,omitempty"`
	LaunchDaemonLabel   string `json:"launch_daemon_label,omitempty"`
	LaunchAgentBarLabel string `json:"launch_agent_bar_label,omitempty"`
}

type DefaultsSection struct {
	// WiFiService is the preferred networksetup service name (e.g. "Wi-Fi"); when it has no IP,
	// the client auto-selects another enabled service (Ethernet, USB LAN, etc.).
	WiFiService string `json:"wifi_service,omitempty"`
}

type ApiSection struct {
	// IPUrl is optional/legacy: when set it is the primary URL. Normally the IP services live in a
	// single ip_urls block whose first entry is treated as the primary (see EffectiveAPI).
	IPUrl  string   `json:"ip_url,omitempty"`
	IPUrls []string `json:"ip_urls,omitempty"` // IP check URLs; first is primary, rest are fallbacks (deduped in ipgeo)
	GeoUrl string   `json:"geo_url,omitempty"`
}

// VPNItem describes the sing-box process managed by the macOS shell.
type VPNItem struct {
	Name string   `json:"name"`
	File string   `json:"file"`
	Host string   `json:"host"`
	Type VPNType  `json:"type"`
	Exec *string  `json:"exec,omitempty"`
	Args []string `json:"args,omitempty"`
}

type AppConfig struct {
	Singbox               SingboxSection    `json:"singbox"`
	VPNs                  []VPNItem         `json:"vpns"`
	DebugLog              string            `json:"debug_log"`
	Connectivity          Connectivity      `json:"connectivity"`
	RequiredTools         []string          `json:"required_tools"`
	StartedListPath       *string           `json:"started_list_path,omitempty"`
	Paths                 *PathsSection     `json:"paths,omitempty"`
	Defaults              *DefaultsSection  `json:"defaults,omitempty"`
	Api                   *ApiSection       `json:"api,omitempty"`
	Autostart             *AutostartSection `json:"autostart,omitempty"`
	DirectBypassProcesses []string          `json:"direct_bypass_processes,omitempty"`
	DirectBypassPaths     []string          `json:"direct_bypass_paths,omitempty"`
	DirectDomains         []string          `json:"direct_domains,omitempty"`
}

// launchDaemonLabel names the daemon's launchd job; the menu bar's agent is the
// same label with ".bar".
const launchDaemonLabel = "one.jorbique.vpn"

type EffectivePaths struct {
	RunDir              string
	RunDirUser          string
	StartedListFilename string
	SingboxFallbackFile string
	LogDir              string
	CfgDir              string
	LaunchDaemonLabel   string
	LaunchAgentBarLabel string
}

func (c *AppConfig) EffectivePaths() EffectivePaths {
	var p *PathsSection = c.Paths
	nonEmpty := func(s string, def string) string {
		t := strings.TrimSpace(s)
		if t == "" {
			return def
		}
		return t
	}
	get := func(f func(*PathsSection) string, def string) string {
		if p == nil {
			return def
		}
		return nonEmpty(f(p), def)
	}
	return EffectivePaths{
		// The daemon's run directory: pid files and the control socket.
		RunDir:              get(func(p *PathsSection) string { return p.RunDir }, InstalledRunDir),
		RunDirUser:          get(func(p *PathsSection) string { return p.RunDirUser }, ".jorbique-vpn"),
		StartedListFilename: get(func(p *PathsSection) string { return p.StartedListFilename }, "started.list"),
		SingboxFallbackFile: get(func(p *PathsSection) string { return p.SingboxFallbackFile }, "singbox.json"),
		LogDir:              get(func(p *PathsSection) string { return p.LogDir }, "logs"),
		CfgDir:              get(func(p *PathsSection) string { return p.CfgDir }, "cfg"),
		LaunchDaemonLabel:   get(func(p *PathsSection) string { return p.LaunchDaemonLabel }, launchDaemonLabel),
		LaunchAgentBarLabel: get(func(p *PathsSection) string { return p.LaunchAgentBarLabel }, launchDaemonLabel+".bar"),
	}
}

type EffectiveAPI struct {
	IPUrl       string
	ExtraIPUrls []string
	GeoUrl      string
}

func (c *AppConfig) EffectiveAPI() EffectiveAPI {
	pick := func(s string, def string) string {
		t := strings.TrimSpace(s)
		if t == "" {
			return def
		}
		return t
	}
	a := c.Api
	mergeExtra := func(in []string) []string {
		var out []string
		seen := make(map[string]struct{})
		for _, s := range in {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
		return out
	}
	if a == nil {
		// api.myip.com is the same service as the myip.com page (JSON); avoids mismatch when
		// sing-box routes per-domain and geojs/ipinfo differ from the site you open in a browser.
		return EffectiveAPI{IPUrl: "https://api.myip.com", GeoUrl: "https://ipinfo.io/json"}
	}
	extra := mergeExtra(a.IPUrls)
	// ip_url and ip_urls are now a single block: the primary URL (fast Swift-style curl) is the
	// explicit ip_url when set, otherwise the first entry of ip_urls. The rest fall through the chain.
	primary := strings.TrimSpace(a.IPUrl)
	if primary == "" && len(extra) > 0 {
		primary = extra[0]
	}
	return EffectiveAPI{
		IPUrl:       pick(primary, "https://api.myip.com"),
		ExtraIPUrls: extra,
		GeoUrl:      pick(a.GeoUrl, "https://ipinfo.io/json"),
	}
}

type EffectiveAutostart struct {
	CooldownSeconds           time.Duration
	WiFiWaitTimeoutSeconds    int
	NetworkChangeDelaySeconds time.Duration
	WakeDelaySeconds          time.Duration
	FallbackTimeoutSeconds    time.Duration
	HeartbeatIntervalSeconds  *int
	SSIDPollIntervalSeconds   *int
	ExcludeNames              map[string]struct{}
	QuietHoursStart           *[2]int
	QuietHoursEnd             *[2]int
}

func parseTime(s string) *[2]int {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ":")
	if len(parts) < 2 {
		return nil
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return nil
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return nil
	}
	return &[2]int{h, m}
}

func (c *AppConfig) EffectiveAutostart() EffectiveAutostart {
	a := c.Autostart
	intOr := func(p *int, def int) int {
		if p != nil {
			return *p
		}
		return def
	}
	floatOr := func(p *float64, def float64) float64 {
		if p != nil {
			return *p
		}
		return def
	}
	excl := map[string]struct{}{}
	if a != nil {
		for _, n := range a.AutostartExclude {
			if n != "" {
				excl[n] = struct{}{}
			}
		}
	}
	out := EffectiveAutostart{
		CooldownSeconds:           90 * time.Second,
		WiFiWaitTimeoutSeconds:    60,
		NetworkChangeDelaySeconds: time.Duration(1.0 * float64(time.Second)),
		WakeDelaySeconds:          time.Duration(2.0 * float64(time.Second)),
		FallbackTimeoutSeconds:    time.Duration(20.0 * float64(time.Second)),
		ExcludeNames:              excl,
	}
	if a == nil {
		return out
	}
	out.CooldownSeconds = time.Duration(intOr(a.CooldownSeconds, 90)) * time.Second
	out.WiFiWaitTimeoutSeconds = intOr(a.WiFiWaitTimeoutSeconds, 60)
	out.NetworkChangeDelaySeconds = time.Duration(floatOr(a.NetworkChangeDelaySeconds, 1.0) * float64(time.Second))
	out.WakeDelaySeconds = time.Duration(floatOr(a.WakeDelaySeconds, 2.0) * float64(time.Second))
	out.FallbackTimeoutSeconds = time.Duration(floatOr(a.FallbackTimeoutSeconds, 20.0) * float64(time.Second))
	out.HeartbeatIntervalSeconds = a.HeartbeatIntervalSeconds
	out.SSIDPollIntervalSeconds = a.SSIDPollIntervalSeconds
	if a.QuietHours != nil {
		out.QuietHoursStart = parseTime(a.QuietHours.Start)
		out.QuietHoursEnd = parseTime(a.QuietHours.End)
	}
	return out
}

func (e EffectiveAutostart) IsInQuietHours(now time.Time) bool {
	if e.QuietHoursStart == nil || e.QuietHoursEnd == nil {
		return false
	}
	h := now.Hour()
	m := now.Minute()
	nowMins := h*60 + m
	startMins := e.QuietHoursStart[0]*60 + e.QuietHoursStart[1]
	endMins := e.QuietHoursEnd[0]*60 + e.QuietHoursEnd[1]
	if startMins <= endMins {
		return nowMins >= startMins && nowMins < endMins
	}
	return nowMins >= startMins || nowMins < endMins
}
