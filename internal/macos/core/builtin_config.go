package core

import "encoding/json"

func ptr[T any](v T) *T { return &v }

// BuiltinConfig returns the full default application configuration, compiled into
// the binary. vpn_cfg.json is only a thin user overlay (route rules and the
// sing-box uuid/outbounds); everything structural — the core process,
// endpoint hosts, the API endpoints, autostart tuning — lives here so it cannot be
// read out of (or tampered with via) the shipped plaintext config. applyOverlay
// merges the user's edits on top of this base.
func BuiltinConfig() *AppConfig {
	cfg := &AppConfig{
		// No outbound list: the downloaded sing-box profile says which protocols
		// exist, and naming them here means shipping last release's answer. An
		// empty list is read as "everything the profile offers"; a user who
		// picks a subset in the menu gets that stored instead.
		Singbox: SingboxSection{},
		VPNs: []VPNItem{
			{Name: "Jorbique VPN", Type: VPNSingbox, File: "cfg/singbox.json", Host: "google.com"},
		},
		Connectivity: Connectivity{DefaultHost: "google.com", Timeout: 5, MaxAttempts: 3, RetryDelay: 5},
		// The autostart list has no default path any more. It used to say
		// "logs/started.list", which after the layout move resolved to a
		// logs directory inside Application Support that nothing creates — so
		// every save failed and the client forgot which tunnels to bring back.
		// Where it goes is the layout's business (Paths.StartedListURL), and a
		// config may still override it.
		RequiredTools: []string{"sing-box"},
		Defaults:      &DefaultsSection{WiFiService: "Wi-Fi"},
		Api: &ApiSection{
			GeoUrl: "https://ipinfo.io/json",
			IPUrls: []string{
				"https://api.myip.com",
				"https://get.geojs.io/v1/ip.json",
				"https://ipinfo.io/json",
				"https://ifconfig.io/all.json",
				"https://ifconfig.co/json",
				"https://ip-api.com/json/?fields=status,message,query,countryCode",
				"https://ipwho.is/",
				"https://ipapi.co/json/",
				"https://v4.api.ipify.org?format=json",
				"https://api.ipify.org?format=json",
				"https://api64.ipify.org?format=json",
				"https://jsonip.com",
				"https://httpbin.org/ip",
				"https://www.cloudflare.com/cdn-cgi/trace",
				"https://1.0.0.1/cdn-cgi/trace",
				"https://1.1.1.1/cdn-cgi/trace",
				"https://api.my-ip.io/v2/ip.txt",
				"https://ifconfig.me/ip",
				"https://ifconfig.io/ip",
				"https://4.ifconfig.me/ip",
				"https://4.icanhazip.com",
				"https://icanhazip.com",
				"https://ident.me",
				"https://4.ident.me",
				"https://ipecho.net/plain",
				"https://l2.io/ip",
				"https://checkip.amazonaws.com",
				"https://myexternalip.com/raw",
				"https://tnx.nl/ip",
				"https://whatismyip.akamai.com",
				"https://wgetip.com",
				"https://ip.tyk.nu",
				"https://4.ifconfig.io/ip",
				"https://api.ipify.org",
			},
		},
		Autostart: &AutostartSection{
			CooldownSeconds:           ptr(90),
			WiFiWaitTimeoutSeconds:    ptr(60),
			NetworkChangeDelaySeconds: ptr(1.0),
			WakeDelaySeconds:          ptr(2.0),
			FallbackTimeoutSeconds:    ptr(20.0),
			HeartbeatIntervalSeconds:  ptr(600),
			SSIDPollIntervalSeconds:   ptr(15),
			AutostartExclude:          []string{},
			QuietHours:                &QuietHoursSection{Start: "23:00", End: "07:00"},
		},
		DirectDomains:         []string{},
		DirectBypassProcesses: []string{},
		DirectBypassPaths:     []string{},
	}
	return cfg
}

// applyOverlay merges the user-editable fields from vpn_cfg.json onto a built-in
// base: the sing-box uuid/outbounds and the direct-route rules. A malformed
// overlay is ignored so the built-in config still stands. Everything else in
// the overlay is intentionally not honoured (it lives only in the binary).
func applyOverlay(base *AppConfig, data []byte) {
	var ov AppConfig
	if err := json.Unmarshal(data, &ov); err != nil {
		return
	}
	base.Singbox.UUID = ov.Singbox.UUID
	if ov.Singbox.EnabledOutbounds != nil {
		base.Singbox.EnabledOutbounds = ov.Singbox.EnabledOutbounds
	}
	// The user's "no" is what decides which outbounds a config is built from;
	// nil means the file predates it and the positive list above is used.
	base.Singbox.DisabledOutbounds = ov.Singbox.DisabledOutbounds
	// Everything else the panels write into this section. Leaving it out was
	// silent: a picked node was saved correctly and read back as the default.
	if ov.Singbox.SelectedNode != "" {
		base.Singbox.SelectedNode = ov.Singbox.SelectedNode
	}
	if ov.DirectDomains != nil {
		base.DirectDomains = ov.DirectDomains
	}
	if ov.DirectBypassProcesses != nil {
		base.DirectBypassProcesses = ov.DirectBypassProcesses
	}
	if ov.DirectBypassPaths != nil {
		base.DirectBypassPaths = ov.DirectBypassPaths
	}
}
