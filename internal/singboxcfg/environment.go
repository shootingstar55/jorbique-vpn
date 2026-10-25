package singboxcfg

import "strings"

// EngineEnvironment keeps a tunnel's HTTP transports from attempting to dial
// through an inherited application proxy (or recursively through the VPN).
func EngineEnvironment(environment []string) []string {
	clean := make([]string, 0, len(environment))
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		switch strings.ToLower(name) {
		case "http_proxy", "https_proxy", "all_proxy":
			continue
		}
		clean = append(clean, entry)
	}
	return clean
}
