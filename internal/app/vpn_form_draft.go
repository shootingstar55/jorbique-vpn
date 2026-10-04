package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"jorbiquevpn/internal/config"
	"jorbiquevpn/internal/paths"
)

// Named in paths because the upgrade reset there has to know to keep it.
const vpnFormDraftFile = paths.VpnFormDraftFileName

type vpnFormDraft struct {
	UpdatedAt       int64           `json:"updated_at"`
	UUID            string          `json:"uuid"`
	DirectDomains   string          `json:"direct_domains"`
	OutboundEnabled map[string]bool `json:"outbound_enabled,omitempty"`
}

func readVpnFormDraft(dataDir string) (*vpnFormDraft, error) {
	path := filepath.Join(dataDir, vpnFormDraftFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var d vpnFormDraft
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

func writeVpnFormDraft(dataDir string, d *vpnFormDraft) error {
	path := filepath.Join(dataDir, vpnFormDraftFile)
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func removeVpnFormDraft(dataDir string) {
	_ = os.Remove(filepath.Join(dataDir, vpnFormDraftFile))
}

// SaveVpnFormDraft writes the current modal fields without validation (debounced from UI).
func (a *App) SaveVpnFormDraft(uuid string, directDomains string, outboundEnabled map[string]bool) (err error) {
	d := &vpnFormDraft{
		UpdatedAt:       time.Now().Unix(),
		UUID:            uuid,
		DirectDomains:   directDomains,
		OutboundEnabled: outboundEnabled,
	}
	return writeVpnFormDraft(a.GetDataDir(), d)
}

func (a *App) effectiveVpnUserForConnect() (uuid string, outboundEnabled map[string]bool, directDomains []string) {
	uuid = strings.TrimSpace(a.store.GetUserUUID())
	outboundEnabled = a.store.GetOutboundEnabled()
	directDomains = a.store.GetDirectDomains()
	savedAt := a.store.GetConfigSavedAt()
	draft, err := readVpnFormDraft(a.GetDataDir())
	if err != nil || draft == nil || draft.UpdatedAt <= savedAt {
		return
	}
	if strings.TrimSpace(draft.UUID) != "" {
		uuid = strings.TrimSpace(draft.UUID)
	}
	if strings.TrimSpace(draft.DirectDomains) != "" {
		if dir, err := config.ParseAndValidateDirectDomains(draft.DirectDomains); err == nil {
			directDomains = dir
		}
	}
	if len(draft.OutboundEnabled) > 0 {
		// The draft answers for what the window showed; the profile decides what
		// else exists, and anything it offers that the draft does not mention is
		// on — the same default a fresh install gets.
		merged := make(map[string]bool, len(draft.OutboundEnabled))
		for _, t := range config.AvailableProxyTags() {
			merged[t] = true
		}
		for t, v := range draft.OutboundEnabled {
			merged[t] = v
		}
		outboundEnabled = merged
	}
	return
}
