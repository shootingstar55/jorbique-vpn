package singboxcfg

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"jorbiquevpn/internal/jsonorder"
)

func parseOutbounds(t *testing.T, body string) []any {
	t.Helper()
	root := jsonorder.New()
	if err := jsonorder.Unmarshal([]byte(`{"outbounds":`+body+`}`), root); err != nil {
		t.Fatal(err)
	}
	list, _ := getSlice(root, "outbounds")
	return list
}

func indexOf(outbounds []any) map[string]*jsonorder.Map {
	return indexOutbounds(outboundMaps(outbounds))
}

func TestProxyTagsFollowsAutoAndStructuralRoles(t *testing.T) {
	objects := parseOutbounds(t, `[
		{"type":"vless","tag":"block","detour":"transport"},
		{"type":"new-protocol","tag":"new"},
		{"type":"tuic","tag":"other"},
		{"type":"shadowtls","tag":"transport","detour":"transport-2"},
		{"type":"new-transport","tag":"transport-2"},
		{"type":"urltest","tag":"earlier","outbounds":["other"]},
		{"type":"new-group","tag":"region","outbounds":["new","block"]},
		{"type":"urltest","tag":"auto","outbounds":["region"]},
		{"type":"selector","tag":"choose","outbounds":["auto","direct"]},
		{"type":"direct","tag":"direct"},
		{"type":"block","tag":"reject"},
		{"type":"dns","tag":"resolver"}
	]`)
	if got, want := ProxyTags(objects, nil), []string{"new", "block", "other"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selectable order = %v, want %v", got, want)
	}
	objects[7].(*jsonorder.Map).Set("outbounds", []any{"region", "transport"})
	if got, want := ProxyTags(objects, nil), []string{"new", "block", "transport", "other"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("explicit transport candidate = %v, want %v", got, want)
	}
}

// Without an "auto" the first urltest still names the order, and a profile that
// leaves the type off is read by the conventional plumbing names.
func TestProxyTagsWithoutAuto(t *testing.T) {
	objects := parseOutbounds(t, `[
		{"type":"vless","tag":"a"},
		{"type":"tuic","tag":"b"},
		{"type":"urltest","tag":"best","outbounds":["b","a"]},
		{"tag":"direct"},
		{"tag":"dns-out"}
	]`)
	if got, want := ProxyTags(objects, nil), []string{"b", "a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selectable order = %v, want %v", got, want)
	}
}

// A helper other proxies only detour through is not a choice, and keeps its
// secret, unless a group traffic can reach names it. A leftover group nothing
// routes to must not promote it.
func TestStrayGroupDoesNotPromoteADetourHelper(t *testing.T) {
	const body = `[
		{"type":"vless","tag":"a","detour":"transport","uuid":""},
		{"type":"tuic","tag":"b","uuid":""},
		{"type":"shadowtls","tag":"transport","password":"server-owned"},
		{"type":"selector","tag":"leftover","outbounds":["transport"]},
		{"type":"selector","tag":"streaming","outbounds":["transport","a"]},
		{"type":"urltest","tag":"auto","outbounds":["a","b"]}
	]`
	if got := ProxyTags(parseOutbounds(t, body), nil); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("unrouted groups promoted the helper: %v", got)
	}
	if got := ProxyTags(parseOutbounds(t, body), []string{"streaming"}); !reflect.DeepEqual(got, []string{"a", "b", "transport"}) {
		t.Errorf("a routed group naming the helper = %v, want it offered", got)
	}
	// With its only detour consumer off, the helper still has groups naming it.
	filtered, err := FilterOutbounds(parseOutbounds(t, body), nil, map[string]bool{"a": false})
	if err != nil {
		t.Fatal(err)
	}
	index := indexOf(filtered)
	if index["transport"] == nil {
		t.Fatalf("helper dropped from under the groups naming it: %v", index)
	}
	if got := stringField(index["transport"], "password"); got != "server-owned" {
		t.Errorf("helper credential = %q", got)
	}
	if got, _ := members(index["streaming"]); !reflect.DeepEqual(got, []string{"transport"}) {
		t.Errorf("streaming candidates = %v", got)
	}
}

// A helper the route, a DNS server or a rule set names directly is still
// needed once the proxies detouring through it are off. Pruning it took the
// rule or the resolver with it, and left the rule set naming nothing.
func TestFilterKeepsAHelperTheProfileNamesDirectly(t *testing.T) {
	for _, extra := range []string{
		`"route":{"rules":[{"outbound":"transport"}]}`,
		`"dns":{"servers":[{"tag":"remote","type":"udp","server":"1.1.1.1","detour":"transport"}]}`,
		`"route":{"rule_set":[{"type":"remote","tag":"ads","url":"https://example.com/ads.srs","download_detour":"transport"}]}`,
	} {
		root := jsonorder.New()
		if err := jsonorder.Unmarshal([]byte(`{"outbounds":[
			{"type":"vless","tag":"a","detour":"transport"},
			{"type":"tuic","tag":"b"},
			{"type":"shadowtls","tag":"transport"},
			{"type":"urltest","tag":"auto","outbounds":["a","b"]}
		],`+extra+`}`), root); err != nil {
			t.Fatal(err)
		}
		outbounds, _ := getSlice(root, "outbounds")
		filtered, err := FilterOutbounds(outbounds, root, map[string]bool{"a": false})
		if err != nil {
			t.Fatalf("%s: %v", extra, err)
		}
		if indexOf(filtered)["transport"] == nil {
			t.Errorf("%s: helper pruned", extra)
		}
	}
}

func TestRouteEntriesFollowNestedRulesButNotDetours(t *testing.T) {
	root := jsonorder.New()
	if err := jsonorder.Unmarshal([]byte(`{
		"route":{"final":"f","rules":[{"outbound":"r1"},{"type":"logical","rules":[{"outbound":"r2"}]}],
			"rule_set":[{"type":"remote","tag":"ads","download_detour":"rs"}]},
		"dns":{"servers":[{"tag":"remote","detour":"d"}]}
	}`), root); err != nil {
		t.Fatal(err)
	}
	if got := RouteEntries(root); !reflect.DeepEqual(got, []string{"f", "r1", "r2"}) {
		t.Errorf("entries = %v", got)
	}
	if got := references(root, true); !reflect.DeepEqual(got, []string{"f", "r1", "r2", "rs", "d"}) {
		t.Errorf("references = %v", got)
	}
	if got := RouteEntries(nil); got != nil {
		t.Errorf("entries of no profile = %v", got)
	}
}

func TestFilterPreservesGroupsAndAddsNewLeaves(t *testing.T) {
	objects := parseOutbounds(t, `[
		{"type":"vless","tag":"a","detour":"transport"},
		{"type":"tuic","tag":"b"},
		{"type":"new-protocol","tag":"fresh"},
		{"type":"vless","tag":"off"},
		{"type":"shadowtls","tag":"transport","password":"server-owned"},
		{"type":"urltest","tag":"empty","outbounds":["off"]},
		{"type":"selector","tag":"empty-parent","outbounds":["empty"]},
		{"type":"new-group","tag":"region","outbounds":["off","b"]},
		{"type":"urltest","tag":"auto","outbounds":["a","region"]},
		{"type":"selector","tag":"choose","outbounds":["empty-parent","auto","direct"],"default":"empty-parent"},
		{"type":"direct","tag":"direct"},
		{"type":"block","tag":"reject"},
		{"type":"dns","tag":"resolver"}
	]`)
	filtered, err := FilterOutbounds(objects, nil, map[string]bool{"off": false})
	if err != nil {
		t.Fatal(err)
	}
	index := indexOf(filtered)
	if index["off"] != nil {
		t.Error("disabled leaf retained")
	}
	// Emptied groups are left for PruneDanglingReferences, which records them.
	for tag, want := range map[string][]string{
		"auto": {"a", "region", "fresh"}, "region": {"b"}, "choose": {"auto", "direct"},
		"empty": nil, "empty-parent": nil,
	} {
		if got, _ := members(index[tag]); !reflect.DeepEqual(got, want) {
			t.Errorf("%s candidates = %v, want %v", tag, got, want)
		}
	}
	if got := stringField(index["choose"], "default"); got != "auto" {
		t.Errorf("selector default = %q", got)
	}
	for _, tag := range []string{"transport", "direct", "reject", "resolver"} {
		if index[tag] == nil {
			t.Errorf("required plumbing %q removed", tag)
		}
	}
	if got := stringField(index["transport"], "password"); got != "server-owned" {
		t.Errorf("transport credential = %q", got)
	}
}

func TestFilterDetourDependenciesAndConflicts(t *testing.T) {
	const body = `[
		{"type":"vless","tag":"a","detour":"helper"},
		{"type":"tuic","tag":"b","detour":"direct"},
		{"type":"shadowtls","tag":"helper","detour":"helper-2"},
		{"type":"new-transport","tag":"helper-2"},
		{"type":"direct","tag":"direct"},
		{"type":"urltest","tag":"auto","outbounds":["a","b"]}
	]`
	filtered, err := FilterOutbounds(parseOutbounds(t, body), nil, map[string]bool{"a": false})
	if err != nil {
		t.Fatal(err)
	}
	index := indexOf(filtered)
	if index["helper"] != nil || index["helper-2"] != nil || index["direct"] == nil {
		t.Fatalf("unused helper chain or required direct detour retained incorrectly: %v", index)
	}
	if got := ProxyTags(filtered, nil); !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("post-filter selectable tags = %v", got)
	}
	// Turning off an outbound an active proxy detours through keeps it, and
	// its own detours, as that proxy's transport, out of every group.
	objects := parseOutbounds(t, body)
	objects[1].(*jsonorder.Map).Set("detour", "a")
	filtered, err = FilterOutbounds(objects, nil, map[string]bool{"a": false})
	if err != nil {
		t.Fatalf("turning off a transport of an active proxy: %v", err)
	}
	index = indexOf(filtered)
	if index["a"] == nil || index["helper"] == nil || index["helper-2"] == nil {
		t.Fatalf("transport chain of the active proxy dropped: %v", index)
	}
	if got, _ := members(index["auto"]); !reflect.DeepEqual(got, []string{"b"}) {
		t.Errorf("auto = %v, want the turned-off outbound out of it", got)
	}
	// Every protocol off is not an error here: nothing selectable and no
	// helper is left, the emptied group is the pruner's, and a config with no
	// tunnel is refused by the build (HasTunnel).
	filtered, err = FilterOutbounds(parseOutbounds(t, body), nil, map[string]bool{"a": false, "b": false})
	if err != nil {
		t.Fatalf("all disabled: %v", err)
	}
	if got := ProxyTags(filtered, nil); len(got) != 0 {
		t.Errorf("all disabled left %v selectable", got)
	}
	if got, _ := members(indexOf(filtered)["auto"]); len(got) != 0 {
		t.Errorf("all disabled left auto = %v", got)
	}
}

// A cycle means nothing to sing-box, so the back edge goes and the profile
// still connects, whether the cycle sits under auto or in a group nobody uses.
// A group the cycle leaves empty is emptied like any other, for the pruner.
func TestFilterBreaksGroupCycles(t *testing.T) {
	filtered, err := FilterOutbounds(parseOutbounds(t, `[
		{"type":"vless","tag":"a"},
		{"type":"tuic","tag":"b"},
		{"type":"urltest","tag":"auto","outbounds":["region","a"]},
		{"type":"selector","tag":"region","outbounds":["auto","b"]},
		{"type":"selector","tag":"unused","outbounds":["loop"]},
		{"type":"selector","tag":"loop","outbounds":["unused"]},
		{"type":"selector","tag":"self","outbounds":["self","a"]}
	]`), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	index := indexOf(filtered)
	for tag, want := range map[string][]string{
		"auto": {"region", "a"}, "region": {"b"}, "self": {"a"},
	} {
		if got, _ := members(index[tag]); !reflect.DeepEqual(got, want) {
			t.Errorf("%s candidates = %v, want %v", tag, got, want)
		}
	}
	for _, tag := range []string{"unused", "loop"} {
		if got, _ := members(index[tag]); len(got) != 0 {
			t.Errorf("%s left by a cycle = %v, want emptied", tag, got)
		}
	}
}

func TestFilterRejectsDuplicateTagsAndMissingDetours(t *testing.T) {
	for body, want := range map[string]string{
		`[{"type":"vless","tag":"a"},{"type":"tuic","tag":"a"}]`: "more than once",
		`[{"type":"vless","tag":"a","detour":"never-existed"}]`:  `"never-existed"`,
	} {
		if _, err := FilterOutbounds(parseOutbounds(t, body), nil, nil); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error = %v, want %q", body, err, want)
		}
	}
}

// Each level has two groups naming the same two below; a walk that revisited
// shared groups per path would take 2^depth steps and never finish.
func TestFilterNestedGroupsStayLinear(t *testing.T) {
	var parts []string
	parts = append(parts, `{"type":"vless","tag":"l0-a"}`, `{"type":"tuic","tag":"l0-b"}`)
	const depth = 40
	for i := 1; i <= depth; i++ {
		children := fmt.Sprintf(`["l%d-a","l%d-b"]`, i-1, i-1)
		parts = append(parts,
			fmt.Sprintf(`{"type":"urltest","tag":"l%d-a","outbounds":%s}`, i, children),
			fmt.Sprintf(`{"type":"urltest","tag":"l%d-b","outbounds":%s}`, i, children))
	}
	parts = append(parts, fmt.Sprintf(`{"type":"urltest","tag":"auto","outbounds":["l%d-a","l%d-b"]}`, depth, depth))
	objects := parseOutbounds(t, "["+strings.Join(parts, ",")+"]")
	if got := ProxyTags(objects, nil); !reflect.DeepEqual(got, []string{"l0-a", "l0-b"}) {
		t.Fatalf("selectable = %v", got)
	}
	if _, err := FilterOutbounds(objects, nil, map[string]bool{"l0-a": false}); err != nil {
		t.Fatal(err)
	}
}

// An empty flow, network or detour asks for sing-box's default; the user's
// credential there breaks the outbound with no build error to say so.
func TestApplyCredentialLeavesEmptyNonCredentialFieldsAlone(t *testing.T) {
	m := outboundMaps(parseOutbounds(t, `[
		{"type":"vless","tag":"v","uuid":"","flow":"","network":"","packet_encoding":"","detour":""}
	]`))[0]
	if got := ApplyCredential(m, "user"); got != 1 {
		t.Errorf("filled %d fields, want 1", got)
	}
	out, _ := m.MarshalJSON()
	if want := `{"type":"vless","tag":"v","uuid":"user","flow":"","network":"","packet_encoding":"","detour":""}`; string(out) != want {
		t.Errorf("outbound = %s, want %s", out, want)
	}
}

func TestApplyCredentialFillsOnlyEmptyFields(t *testing.T) {
	maps := outboundMaps(parseOutbounds(t, `[
		{"type":"tuic","tag":"tuic","uuid":"","password":"server-shared"},
		{"type":"hysteria2","tag":"hy2","password":""},
		{"type":"vless","tag":"bare","tls":{"reality":{"short_id":""}}}
	]`))
	for i, want := range []int{1, 1, 0} {
		if got := ApplyCredential(maps[i], "user"); got != want {
			t.Errorf("%s filled %d fields, want %d", stringField(maps[i], "tag"), got, want)
		}
	}
	if stringField(maps[0], "uuid") != "user" || stringField(maps[0], "password") != "server-shared" {
		t.Errorf("tuic = %v", maps[0])
	}
	if stringField(maps[1], "password") != "user" {
		t.Errorf("hysteria2 = %v", maps[1])
	}
}

func TestExplicitCredentialMarkersPreserveSudokuKeyAndTLS(t *testing.T) {
	const user = "a8ad6435-7922-4cc6-ab54-9697cf4e7f49"
	objects := outboundMaps(parseOutbounds(t, `[
		{"type":"sudoku","tag":"sudoku","key":"server-shared-key","http_mask":{"path_root":"__JORBIQUE_USER_UUID__","host":"media.example.test","tls":{"server_name":"media.example.test","reality":{"short_id":""}}}},
		{"type":"mieru","tag":"mieru","username":"__JORBIQUE_USER_UUID__","password":"__JORBIQUE_USER_UUID__"},
		{"type":"sudoku","tag":"fixed","key":"server-key","http_mask":{"path_root":"fixed-by-server"}},
		{"type":"sudoku","tag":"missing-key","key":"","http_mask":{"path_root":"__JORBIQUE_USER_UUID__"}}
	]`))
	for i, want := range []int{1, 2, 0, 1} {
		if got := ApplyCredential(objects[i], user); got != want {
			t.Fatalf("outbound %d: filled %d, want %d", i, got, want)
		}
	}
	if objects[3].GetString("key") != "" {
		t.Fatal("an empty server key received the UUID despite an explicit credential marker")
	}
	mask, _ := objects[0].Get("http_mask")
	m := mask.(*jsonorder.Map)
	if m.GetString("path_root") != user || m.GetString("host") != "media.example.test" || objects[0].GetString("key") != "server-shared-key" {
		t.Fatal("server-owned Sudoku configuration was rewritten")
	}
	tls, _ := m.Get("tls")
	reality, _ := tls.(*jsonorder.Map).Get("reality")
	if reality.(*jsonorder.Map).GetString("short_id") != "" || tls.(*jsonorder.Map).GetString("server_name") != "media.example.test" {
		t.Fatal("nested TLS settings were rewritten")
	}
}

// Bringing an outbound back walks the profile; the same profile must build the
// same config every time, whatever the map order.
func TestFilterIsDeterministic(t *testing.T) {
	const body = `[
		{"type":"vless","tag":"l1"},
		{"type":"vless","tag":"l2","detour":"g0"},
		{"type":"direct","tag":"direct"},
		{"type":"vless","tag":"l0"},
		{"type":"urltest","tag":"g2","outbounds":["l0","l2","g0"]},
		{"type":"urltest","tag":"g1","outbounds":["g0","g2"]},
		{"type":"urltest","tag":"g0","outbounds":["g2","g0","g1"]},
		{"type":"urltest","tag":"auto","outbounds":["l0","l1","l2"]}
	]`
	var first string
	for i := 0; i < 50; i++ {
		filtered, err := FilterOutbounds(parseOutbounds(t, body), nil, map[string]bool{"l0": false})
		got := fmt.Sprint(err)
		if err == nil {
			data, _ := json.Marshal(filtered)
			got = string(data)
		}
		if i == 0 {
			first = got
		} else if got != first {
			t.Fatalf("run %d built %s, first run built %s", i, got, first)
		}
	}
}

// What the user turned off is never picked on its own, but a proxy still in use
// that detours through it keeps it as a transport, directly or through a group.
// The route naming it is still the profile's mistake, as for any outbound
// turned off.
func TestFilterKeepsATurnedOffTransport(t *testing.T) {
	for _, tc := range []struct {
		name, group, detour, route string
		kept, want                 []string
		err                        string
	}{
		{name: "direct detour", detour: "x", kept: []string{"x"}},
		{name: "detour through a group", group: `,{"type":"selector","tag":"g","outbounds":["x","gone","g"],"default":"gone"}`,
			detour: "g", kept: []string{"x", "g"}, want: []string{"g:x"}},
		{name: "routed directly", detour: "x", route: `,"route":{"rules":[{"outbound":"x"}]}`, err: `"x", which is turned off`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := jsonorder.New()
			if err := jsonorder.Unmarshal([]byte(`{"outbounds":[
				{"type":"vless","tag":"x"},
				{"type":"tuic","tag":"y"}`+tc.group+`,
				{"type":"vless","tag":"p","detour":"`+tc.detour+`"},
				{"type":"urltest","tag":"auto","outbounds":["x","y","p"]}
			]`+tc.route+`}`), root); err != nil {
				t.Fatal(err)
			}
			outbounds, _ := getSlice(root, "outbounds")
			filtered, err := FilterOutbounds(outbounds, root, map[string]bool{"x": false})
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("error = %v, want %q", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			index := indexOf(filtered)
			if got, _ := members(index["auto"]); slices.Contains(got, "x") {
				t.Errorf("auto = %v, the turned-off outbound picked on its own", got)
			}
			// Transports go last, so a route without final cannot default to one.
			var tail []string
			for _, m := range outboundMaps(filtered[len(filtered)-len(tc.kept):]) {
				tail = append(tail, stringField(m, "tag"))
			}
			if !reflect.DeepEqual(tail, tc.kept) {
				t.Errorf("last outbounds = %v, want %v", tail, tc.kept)
			}
			for _, w := range tc.want {
				group, member, _ := strings.Cut(w, ":")
				got, _ := members(index[group])
				if d := stringField(index[group], "default"); !reflect.DeepEqual(got, []string{member}) || d != "" && d != member {
					t.Errorf("%s = %v default %q, want only %s", group, got, d, member)
				}
			}
		})
	}
}

// sing-box will not start on a detour cycle; the build says so, by name.
func TestFilterRejectsADetourCycle(t *testing.T) {
	_, err := FilterOutbounds(parseOutbounds(t, `[
		{"type":"vless","tag":"a","detour":"b"},
		{"type":"vless","tag":"b","detour":"a"},
		{"type":"urltest","tag":"auto","outbounds":["a"]}
	]`), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "detour cycle") {
		t.Fatalf("error = %v, want a detour cycle named", err)
	}
}

// What no build of a profile can get past is reported before the profile is
// trusted, whatever the user turned off.
func TestCheckProfile(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{"outbounds":[{"type":"vless","tag":"a","uuid":""},{"type":"urltest","tag":"auto","outbounds":["a"]}],"route":{"final":"auto"}}`, ""},
		{`{"outbounds":[{"type":"snell","tag":"s","userkey":""},{"type":"hysteria","tag":"h","auth":"shared"}]}`, ""},
		{`{"outbounds":[{"type":"hysteria","tag":"h","auth":"shared"}]}`, "no field"},
		{`{"outbounds":[{"type":"hysteria","tag":"h","auth":""}]}`, ""},
		{`{"outbounds":[{"type":"direct","tag":"direct"}]}`, "no proxy outbounds"},
		{`{"outbounds":[null,{"type":"vless","tag":"a","uuid":""}]}`, "outbound 0 is not an object"},
		{`{"outbounds":[{"type":"vless","tag":"a","uuid":""},{"type":"tuic","tag":"a"}]}`, "more than once"},
		{`{"outbounds":[{"type":"vless","tag":"a","uuid":"","detour":"gone"}]}`, `"gone"`},
		{`{"outbounds":[{"type":"vless","tag":"a","uuid":"","detour":"b"},{"type":"vless","tag":"b","detour":"a"},{"type":"urltest","tag":"auto","outbounds":["a","b"]}]}`, "detour cycle"},
	} {
		root := jsonorder.New()
		if err := jsonorder.Unmarshal([]byte(tc.body), root); err != nil {
			t.Fatal(err)
		}
		err := CheckProfile(root)
		if tc.want == "" && err != nil {
			t.Errorf("%s: %v", tc.body, err)
		}
		if tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("%s: error = %v, want %q", tc.body, err, tc.want)
		}
	}
}
