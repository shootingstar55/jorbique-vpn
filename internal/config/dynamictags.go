package config

import (
	"jorbiquevpn/internal/singboxcfg"
	"os"

	"jorbiquevpn/internal/jsonorder"
)

// ProxyTagsFromConfig lists the tags a user can choose between, in the order
// the profile means them to be tried. The rule is the same on both clients, so
// it lives in singboxcfg and this reads the outbounds and the route out of the
// document.
func ProxyTagsFromConfig(cfg *jsonorder.Map) []string {
	if cfg == nil {
		return nil
	}
	obsRaw, _ := cfg.Get("outbounds")
	obs, _ := obsRaw.([]any)
	return singboxcfg.ProxyTags(obs, singboxcfg.RouteEntries(cfg))
}

// AvailableProxyTags lists what the last downloaded profile offers, for the UI
// and for the checks that run before a connection. Reads the cache: the window
// must not wait on the network to draw a list of checkboxes.
func AvailableProxyTags() []string {
	data, err := os.ReadFile(RemoteSingboxCachePath())
	if err != nil || len(data) == 0 {
		return nil
	}
	cfg := jsonorder.New()
	if jsonorder.Unmarshal(data, cfg) != nil {
		return nil
	}
	return ProxyTagsFromConfig(cfg)
}

// ProxyTagsFromJSON is ProxyTagsFromConfig for a profile still in its raw
// bytes, which is how the Windows client carries it between fetching and
// building.
func ProxyTagsFromJSON(remoteJSON []byte) []string {
	if len(remoteJSON) == 0 {
		return nil
	}
	cfg := jsonorder.New()
	if jsonorder.Unmarshal(remoteJSON, cfg) != nil {
		return nil
	}
	return ProxyTagsFromConfig(cfg)
}

// getSlice reads an array field, reporting whether it was there and an array.
func getSlice(m *jsonorder.Map, key string) ([]any, bool) {
	v, ok := m.Get(key)
	if !ok {
		return nil, false
	}
	list, ok := v.([]any)
	return list, ok
}

// getMap reads an object field.
func getMap(m *jsonorder.Map, key string) (*jsonorder.Map, bool) {
	v, ok := m.Get(key)
	if !ok {
		return nil, false
	}
	obj, ok := v.(*jsonorder.Map)
	return obj, ok
}
