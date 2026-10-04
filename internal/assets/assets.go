package assets

import "embed"

//go:embed tun_system.json tun_gvisor.json tun_mixed.json singbox_cfg.yml
var FS embed.FS
