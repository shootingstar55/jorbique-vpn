package singboxcfg

import (
	"reflect"
	"testing"

	"jorbiquevpn/internal/jsonorder"
)

// The order the runtime config came out in before this: the profile's own
// sections first, then whatever the client appended.
func TestSortRootPutsTheSectionsInSchemaOrder(t *testing.T) {
	cfg := jsonorder.New()
	for _, k := range []string{"dns", "endpoints", "outbounds", "route", "log", "inbounds", "services", "experimental"} {
		cfg.Set(k, k)
	}
	SortRoot(cfg)

	want := []string{"log", "dns", "endpoints", "inbounds", "outbounds", "route", "services", "experimental"}
	if got := cfg.Keys(); !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
	// Sorting moves keys; it must not lose or invent values.
	if v, _ := cfg.Get("route"); v != "route" || cfg.Len() != len(want) {
		t.Errorf("sorting changed the content: %v / %d keys", v, cfg.Len())
	}
}

// A section this client has never heard of is the server's to add, and it has
// to survive the sort rather than be dropped or shuffled among its own kind.
func TestSortRootKeepsUnknownSectionsAtTheEndInOrder(t *testing.T) {
	cfg := jsonorder.New()
	for _, k := range []string{"zeta_thing", "outbounds", "alpha_thing", "log"} {
		cfg.Set(k, k)
	}
	SortRoot(cfg)

	want := []string{"log", "outbounds", "zeta_thing", "alpha_thing"}
	if got := cfg.Keys(); !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}
