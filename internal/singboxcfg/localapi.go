package singboxcfg

import "jorbiquevpn/internal/jsonorder"

// RemoveLocalAPI drops sing-box's local API from a config: every "api" service,
// and the HTTP client its downloadable dashboard fetched itself with. The client
// reads nothing from a running core, so a listening port guarded by a secret is
// surface it does not need, whether the local template or the profile adds it.
func RemoveLocalAPI(root *jsonorder.Map) {
	removeFromList(root, "services", func(item *jsonorder.Map) bool { return item.GetString("type") == "api" })
	removeFromList(root, "http_clients", func(item *jsonorder.Map) bool { return item.GetString("tag") == "dashboard-http" })
}

// removeFromList drops the entries of root[key] that drop reports, and the key
// itself once nothing is left in it.
func removeFromList(root *jsonorder.Map, key string, drop func(*jsonorder.Map) bool) {
	raw, ok := root.Get(key)
	if !ok {
		return
	}
	list, _ := raw.([]any)
	kept := make([]any, 0, len(list))
	for _, item := range list {
		if m, ok := item.(*jsonorder.Map); ok && drop(m) {
			continue
		}
		kept = append(kept, item)
	}
	if len(kept) == 0 {
		root.Delete(key)
		return
	}
	root.Set(key, kept)
}
