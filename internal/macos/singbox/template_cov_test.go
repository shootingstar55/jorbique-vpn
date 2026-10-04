package singbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"jorbiquevpn/internal/macos/core"
)

// Only string ranges are rewritten; a numeric port is left as it came.
func TestNormalizePortRangesLeavesNonStrings(t *testing.T) {
	ports := []any{json.Number("443"), " 2080-3000 ", "5000"}
	normalizePortRanges(ports)
	want := []any{json.Number("443"), "2080:3000", "5000"}
	if !reflect.DeepEqual(ports, want) {
		t.Errorf("ports = %v, want %v", ports, want)
	}
}

// A local base that does not parse is replaced by the built-in local half
// instead of failing the build.
func TestLocalRootOrDefaultOnUnparsableFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "singbox.json")
	if err := os.WriteFile(p, []byte(`{"log":`), 0o644); err != nil {
		t.Fatal(err)
	}
	root := localRootOrDefault(p)
	if !reflect.DeepEqual(root.Keys(), defaultLocalRoot().Keys()) {
		t.Errorf("keys = %v, want the default local root", root.Keys())
	}
	inbounds, _ := root.Get("inbounds")
	if list, _ := inbounds.([]any); len(list) != 1 || list[0].(*OrderedMap).GetString("type") != "tun" {
		t.Errorf("inbounds = %v, want the default TUN inbound", inbounds)
	}
}

// The client runs proxies only: a profile's endpoints leave the built config,
// and so does whatever resolved through them or routed to them.
func TestBuildConfigFromRemoteTemplateDropsEndpoints(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "singbox.json")
	profile := `{"outbounds":[{"type":"vless","tag":"vless-reality","uuid":""},{"type":"direct","tag":"direct"}],` +
		`"endpoints":[{"type":"openvpn-client","tag":"ovpn-intra"},{"type":"wireguard","tag":"wg"}],` +
		`"dns":{"servers":[{"tag":"doh","type":"https","server":"1.1.1.1"},{"tag":"intra-dns","type":"openvpn","endpoint":"ovpn-intra"}],` +
		`"rules":[{"domain_suffix":["lan"],"server":"intra-dns"}],"final":"doh"},` +
		`"route":{"rules":[{"ip_cidr":["10.0.0.0/8"],"outbound":"ovpn-intra"}],"final":"vless-reality"}}`
	if err := os.WriteFile(remoteTemplateCachePath(base), []byte(profile), 0o644); err != nil {
		t.Fatal(err)
	}

	// A 1ns timeout stands in for an unreachable server; the cache above is used.
	root, err := BuildConfigFromRemoteTemplate(core.SingboxSection{UUID: "11111111-1111-1111-1111-111111111111"}, base, time.Nanosecond, nil)
	if err != nil {
		t.Fatal(err)
	}
	if root.Has("endpoints") {
		t.Error("the endpoints reached the config")
	}
	rendered, _ := json.Marshal(root)
	for _, gone := range []string{"ovpn-intra", "intra-dns", "10.0.0.0/8"} {
		if strings.Contains(string(rendered), gone) {
			t.Errorf("%s survived:\n%s", gone, rendered)
		}
	}
}

func TestTemplateOutboundsRejectsBrokenProfiles(t *testing.T) {
	for _, tc := range []struct {
		name, profile, want string
	}{
		{"no outbounds", `{}`, "template missing outbounds"},
		{"outbounds not an array", `{"outbounds":{}}`, "template outbounds is not array"},
		{"no proxies", `{"outbounds":[{"type":"direct","tag":"direct"}]}`, "no proxy outbounds"},
		{"duplicate tag", `{"outbounds":[{"type":"vless","tag":"a","uuid":""},{"type":"vless","tag":"a","uuid":""}]}`, "more than once"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := NewOrderedMap()
			if err := root.UnmarshalJSON([]byte(tc.profile)); err != nil {
				t.Fatal(err)
			}
			_, err := templateOutbounds(root, core.SingboxSection{UUID: "u"}, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want one containing %q", err, tc.want)
			}
		})
	}
}

// A tag the configuration still names but the server dropped is reported once.
func TestTemplateOutboundsWarnsAboutVanishedTags(t *testing.T) {
	root := NewOrderedMap()
	if err := root.UnmarshalJSON([]byte(`{"outbounds":[{"type":"vless","tag":"vless-reality","uuid":""},{"type":"direct","tag":"direct"}]}`)); err != nil {
		t.Fatal(err)
	}
	var warnings []string
	settings := core.SingboxSection{UUID: "u", EnabledOutbounds: []string{"vless-reality", "gone"}}
	obs, err := templateOutbounds(root, settings, func(m string) { warnings = append(warnings, m) })
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || !strings.HasSuffix(warnings[0], "no longer offers gone") {
		t.Errorf("warnings = %q", warnings)
	}
	if len(obs) == 0 || obs[0].(*OrderedMap).GetString("uuid") != "u" {
		t.Errorf("outbounds = %v, want vless-reality with the credential", obs)
	}
}
