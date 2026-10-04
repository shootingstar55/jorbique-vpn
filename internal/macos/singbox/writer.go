package singbox

import (
	"encoding/json"
	"fmt"
	"os"

	"jorbiquevpn/internal/jsonorder"
	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/singboxcfg"
)

func WriteJSONFileAtomically(root *OrderedMap, path string) error {
	if root == nil || root.Len() == 0 {
		return core.BadConfig("empty sing-box JSON for " + path)
	}
	// Written in the order it was read. The writer used to impose its own key
	// order — top-level, then per-object lists copied from the documentation —
	// so the config on disk listed the same fields in a different sequence from
	// the profile the server serves, which made the two impossible to compare.
	singboxcfg.SortRoot(root)
	out, err := jsonorder.MarshalIndent(root)
	if err != nil {
		return core.BadConfig("cannot render sing-box JSON for " + path + ": " + err.Error())
	}
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return core.BadConfig("cannot write sing-box JSON to " + tmp + ": " + err.Error())
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return core.BadConfig("cannot replace sing-box JSON at " + path + ": " + err.Error())
	}
	return nil
}

// LoadOrderedRoot loads singbox JSON preserving key order.
func LoadOrderedRoot(path string) (*OrderedMap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	root := NewOrderedMap()
	if err := json.Unmarshal(core.StripTrailingCommas(data), root); err != nil {
		return nil, err
	}
	return root, nil
}
