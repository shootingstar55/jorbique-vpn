package singboxcfg

import (
	"reflect"
	"testing"

	"jorbiquevpn/internal/jsonorder"
)

func naiveModeOutbounds(t *testing.T) []any {
	t.Helper()
	root := jsonorder.New()
	if err := jsonorder.Unmarshal([]byte(`{"outbounds":[
	  {"type":"vless","tag":"vless-reality"},
	  {"type":"tuic","tag":"tuic"},
	  {"type":"naive","tag":"nvp"},
	  {"type":"urltest","tag":"auto","outbounds":["vless-reality","tuic"]},
	  {"type":"direct","tag":"direct"}]}`), root); err != nil {
		t.Fatal(err)
	}
	raw, _ := root.Get("outbounds")
	return raw.([]any)
}

// naive is found by its type: the tag is whatever the server called it.
func TestNaiveTagsGoByType(t *testing.T) {
	if got := NaiveTags(naiveModeOutbounds(t)); !reflect.DeepEqual(got, []string{"nvp"}) {
		t.Errorf("NaiveTags = %v, want [nvp]", got)
	}
}

func TestProtocolTagsLeaveNaiveOut(t *testing.T) {
	obs := naiveModeOutbounds(t)
	if got := ProxyTags(obs, nil); !reflect.DeepEqual(got, []string{"vless-reality", "tuic", "nvp"}) {
		t.Fatalf("ProxyTags = %v, want naive among the proxies", got)
	}
	if got := ProtocolTags(obs, nil); !reflect.DeepEqual(got, []string{"vless-reality", "tuic"}) {
		t.Errorf("ProtocolTags = %v, want the protocols without naive", got)
	}
}

func TestModeEnabled(t *testing.T) {
	obs := naiveModeOutbounds(t)
	available := ProxyTags(obs, nil)

	// The NaiveProxy mode: naive alone, whatever the toggles say.
	got := ModeEnabled(obs, available, map[string]bool{"tuic": true, "nvp": false}, true)
	if want := map[string]bool{"vless-reality": false, "tuic": false, "nvp": true}; !reflect.DeepEqual(got, want) {
		t.Errorf("naive mode = %v, want %v", got, want)
	}

	// The protocols mode: the toggles as they are, naive off even when its
	// toggle says on, and nothing answered for the rest (which means on).
	got = ModeEnabled(obs, available, map[string]bool{"tuic": false, "nvp": true}, false)
	if want := map[string]bool{"tuic": false, "nvp": false}; !reflect.DeepEqual(got, want) {
		t.Errorf("protocols mode = %v, want %v", got, want)
	}
	if got := ModeEnabled(obs, available, nil, false); !reflect.DeepEqual(got, map[string]bool{"nvp": false}) {
		t.Errorf("protocols mode with no toggles = %v, want naive off and the rest unanswered", got)
	}
}

// Through FilterOutbounds, the NaiveProxy mode leaves "auto" failing over to
// naive alone even though the profile kept naive out of it.
func TestNaiveModeFillsAutoWithNaive(t *testing.T) {
	obs := naiveModeOutbounds(t)
	filtered, err := FilterOutbounds(obs, nil, ModeEnabled(obs, ProxyTags(obs, nil), nil, true))
	if err != nil {
		t.Fatal(err)
	}
	var tags []string
	for _, m := range outboundMaps(filtered) {
		tags = append(tags, stringField(m, "tag"))
		if stringField(m, "tag") == "auto" {
			if list, _ := members(m); !reflect.DeepEqual(list, []string{"nvp"}) {
				t.Errorf("auto = %v, want [nvp]", list)
			}
		}
	}
	if !reflect.DeepEqual(tags, []string{"nvp", "auto", "direct"}) {
		t.Errorf("outbounds = %v, want naive, auto and direct", tags)
	}
}
