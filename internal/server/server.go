package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"jorbiquevpn/internal/config"
	"jorbiquevpn/internal/paths"
	"jorbiquevpn/internal/security"
	"jorbiquevpn/internal/vless"
)

// Server represents a proxy server
type Server struct {
	ID      string `json:"id"`
	Type    string `json:"type"` // vless, vmess, shadowsocks, trojan
	Name    string `json:"name"`
	Address string `json:"address"`
	Port    int    `json:"port"`
	URL     string `json:"url,omitempty"` // Full URL for parsing
	Source  string `json:"source"`        // "manual" or subscription URL
}

// Subscription represents a subscription source
type Subscription struct {
	URL     string   `json:"url"`
	Name    string   `json:"name"`
	Servers []Server `json:"servers"`
}

// Store holds servers and subscriptions
type Store struct {
	Servers             []Server        `json:"servers"`
	Subscriptions       []Subscription  `json:"subscriptions"`
	UserUUID            string          `json:"user_uuid,omitempty"`
	OutboundEnabled     map[string]bool `json:"outbound_enabled,omitempty"` // keyed by outbound tag, e.g. vless-reality, anytls
	SelectedNode        string          `json:"selected_node,omitempty"`    // the selector node the user picked, re-applied on every start
	NaiveMode           bool            `json:"naive_mode,omitempty"`       // connect through the profile's naive outbound instead of its protocols
	IgnoredProcessNames []string        `json:"ignored_process_names,omitempty"`
	IgnoredProcessPaths []string        `json:"ignored_process_paths,omitempty"` // full paths for sing-box process_path
	DirectDomains       []string        `json:"direct_domains,omitempty"`
	ConfigSavedAt       int64           `json:"config_saved_at,omitempty"` // last successful SetVpnUserConfig; draft older than this loses

	VlessSavedAtLegacy int64 `json:"vless_saved_at,omitempty"` // read once; migrated to config_saved_at then cleared

	mu sync.RWMutex
}

// Named in paths because the upgrade reset there has to know to keep it.
const serversFileName = paths.StoreFileName

// Load loads server store from dataDir
func Load(dataDir string) (*Store, error) {
	path := filepath.Join(dataDir, serversFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Store{}, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return &Store{}, nil
	}
	var s Store
	if err := json.Unmarshal(data, &s); err != nil {
		backupPath := path + ".corrupt." + time.Now().Format("20060102-150405")
		if rerr := os.Rename(path, backupPath); rerr != nil {
			return &Store{}, fmt.Errorf("servers.json: invalid JSON: %w (could not save backup: %v)", err, rerr)
		}
		return &Store{}, fmt.Errorf("servers.json: invalid JSON (backup: %s): %w", filepath.Base(backupPath), err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err == nil {
		migrateLegacyVlessURLsFromRaw(&s, raw)
	}
	if s.ConfigSavedAt == 0 && s.VlessSavedAtLegacy != 0 {
		s.ConfigSavedAt = s.VlessSavedAtLegacy
	}
	s.VlessSavedAtLegacy = 0
	if s.ConfigSavedAt == 0 && strings.TrimSpace(s.UserUUID) != "" {
		s.ConfigSavedAt = time.Now().Unix()
		_ = s.Save(dataDir)
	}
	return &s, nil
}

// migrateLegacyVlessURLsFromRaw reads legacy "vless_urls" from old servers.json (never written again).
func migrateLegacyVlessURLsFromRaw(s *Store, raw map[string]interface{}) {
	if strings.TrimSpace(s.UserUUID) != "" || raw == nil {
		return
	}
	vu, ok := raw["vless_urls"].(map[string]interface{})
	if !ok {
		return
	}
	for _, key := range []string{"reality", "grpc", "anytls"} {
		u, _ := vu[key].(string)
		if id := extractUUIDFromLegacyURL(u); id != "" {
			s.UserUUID = id
			return
		}
	}
}

func extractUUIDFromLegacyURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(u, "vless://"):
		if p, err := vless.ParseVlessGrpc(u); err == nil && p != nil {
			return p.UUID
		}
	case strings.HasPrefix(u, "anytls://"):
		if p, err := vless.ParseAnyTLS(u); err == nil && p != nil && security.ValidUUID(p.Password, false) {
			return p.Password
		}
	}
	return ""
}

// Save persists store to dataDir
func (s *Store) Save(dataDir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(dataDir, serversFileName)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// SetUserUUIDAndOutbounds updates stored user UUID and per-outbound toggles (nil map = all enabled).
func (s *Store) SetUserUUIDAndOutbounds(userUUID string, outboundEnabled map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.UserUUID = strings.TrimSpace(userUUID)
	if outboundEnabled == nil {
		return
	}
	s.OutboundEnabled = make(map[string]bool, len(outboundEnabled))
	for k, v := range outboundEnabled {
		s.OutboundEnabled[strings.TrimSpace(k)] = v
	}
}

// GetUserUUID returns stored user UUID.
func (s *Store) GetUserUUID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.UserUUID
}

// GetOutboundEnabled returns the stored per-tag choices, filled in for every
// outbound the downloaded profile currently offers: a tag with no stored
// answer is on, which is how a protocol the server adds starts working without
// anyone opening the settings.
//
// Tags the profile no longer has are not carried over — they would sit in the
// saved settings for ever, and nothing can be routed through them.
func (s *Store) GetOutboundEnabled() map[string]bool {
	return s.OutboundEnabledFor(config.AvailableProxyTags())
}

// OutboundEnabledFor is GetOutboundEnabled against a given list of outbounds,
// so callers that already read the profile do not read it again — and so this
// can be tested without a downloaded one.
func (s *Store) OutboundEnabledFor(available []string) map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(available) == 0 {
		// No profile downloaded yet: the stored answers are all that is known,
		// and dropping them here would lose the user's choices on a first run
		// with no network.
		out := make(map[string]bool, len(s.OutboundEnabled))
		for k, v := range s.OutboundEnabled {
			out[k] = v
		}
		return out
	}

	out := make(map[string]bool, len(available))
	for _, t := range available {
		out[t] = config.OutboundEnabled(s.OutboundEnabled, t)
	}
	return out
}

// SetIgnoredProcessNames overwrites the ignored process_name list (case preserved, uniqueness enforced by caller).
func (s *Store) SetIgnoredProcessNames(names []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.IgnoredProcessNames = names
}

// GetNaiveMode reports whether the client connects in the NaiveProxy mode:
// the profile's naive outbound alone, instead of its other protocols.
func (s *Store) GetNaiveMode() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.NaiveMode
}

// SetNaiveMode switches between the NaiveProxy mode and the protocols.
func (s *Store) SetNaiveMode(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.NaiveMode = on
}

// GetSelectedNode returns the selector node the user picked, empty when they
// never picked one — which leaves the profile's own default in charge.
func (s *Store) GetSelectedNode() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.SelectedNode
}

// GetIgnoredProcessNames returns the ignored process_name list.
func (s *Store) GetIgnoredProcessNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return returnedStrings(s.IgnoredProcessNames)
}

// SetIgnoredProcessPaths overwrites the ignored process_path list (full paths to executables).
func (s *Store) SetIgnoredProcessPaths(paths []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.IgnoredProcessPaths = storedStrings(paths)
}

// GetIgnoredProcessPaths returns stored full paths for sing-box process_path rules.
func (s *Store) GetIgnoredProcessPaths() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return returnedStrings(s.IgnoredProcessPaths)
}

// SetDirectDomains replaces saved user domain suffixes for direct routing.
func (s *Store) SetDirectDomains(domains []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.DirectDomains = storedStrings(domains)
}

// GetDirectDomains returns a copy of user direct domain suffixes.
func (s *Store) GetDirectDomains() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return returnedStrings(s.DirectDomains)
}

func storedStrings(values []string) []string {
	return append([]string(nil), values...)
}

func returnedStrings(values []string) []string {
	out := make([]string, 0, len(values))
	return append(out, values...)
}

// SetConfigSavedAt records when settings were last validated and saved (for draft vs store precedence).
func (s *Store) SetConfigSavedAt(t int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ConfigSavedAt = t
}

// GetConfigSavedAt returns unix time of last successful SetVpnUserConfig, or 0.
func (s *Store) GetConfigSavedAt() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ConfigSavedAt
}
