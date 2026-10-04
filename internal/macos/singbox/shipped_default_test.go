package singbox

import (
	"reflect"
	"testing"
)

// theShippedFive is the list the .pkg used to install. Written out here rather
// than read from shippedDefaultOutbounds so that changing that variable fails
// this test instead of quietly moving what counts as "unchosen".
var theShippedFive = []string{"vless-reality", "vless-grpc", "tuic", "hysteria2", "anytls"}

func TestIsUnchosenShippedDefault(t *testing.T) {
	for _, c := range []struct {
		name     string
		enabled  []string
		disabled []string
		want     bool
	}{
		{
			// The file every machine installed before the overlay was emptied
			// is still carrying.
			name:    "the packaged list, never written by a client",
			enabled: theShippedFive,
			want:    true,
		},
		{
			// Order is not part of the file's meaning.
			name:    "same five in another order",
			enabled: []string{"anytls", "tuic", "vless-grpc", "hysteria2", "vless-reality"},
			want:    true,
		},
		{
			name:    "same five, written loosely",
			enabled: []string{"VLESS-Reality", " vless-grpc ", "TUIC", "hysteria2", "anytls"},
			want:    true,
		},
		{
			// A recorded "no" means a client that understands refusals wrote
			// this section, so the positive list beside it was chosen. Empty
			// counts: it is a key, and only a write puts one there.
			name:     "the same five, but a refusal was recorded",
			enabled:  theShippedFive,
			disabled: []string{},
			want:     false,
		},
		{
			name:     "the same five, with something refused",
			enabled:  theShippedFive,
			disabled: []string{"naive"},
			want:     false,
		},
		{
			// A real choice: the user unticked one of the five on a client old
			// enough not to record refusals. Nothing may touch this.
			name:    "a hand-picked subset",
			enabled: []string{"vless-reality", "tuic"},
			want:    false,
		},
		{
			// Everything the profile offered at the time plus one the user
			// turned on later — still a choice, not the shipped file.
			name:    "the five and one more",
			enabled: append(append([]string{}, theShippedFive...), "naive"),
			want:    false,
		},
		{
			name:    "four of the five",
			enabled: []string{"vless-reality", "vless-grpc", "tuic", "hysteria2"},
			want:    false,
		},
		{
			// Same length, different tags.
			name:    "five tags that are not those five",
			enabled: []string{"vless-reality", "vless-grpc", "tuic", "hysteria2", "naive"},
			want:    false,
		},
		{
			name:    "a duplicate padding the count",
			enabled: []string{"vless-reality", "vless-reality", "tuic", "hysteria2", "anytls"},
			want:    false,
		},
		{name: "nothing stored", enabled: nil, want: false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := IsUnchosenShippedDefault(c.enabled, c.disabled); got != c.want {
				t.Errorf("IsUnchosenShippedDefault(%v, %v) = %v, want %v", c.enabled, c.disabled, got, c.want)
			}
		})
	}
}

// The point of the whole rule: a machine that has carried the packaged list
// since before the fix picks up an outbound the server adds, instead of it
// being off forever because a file it never wrote says so.
func TestEnabledProxyTagsGivesAnUnchosenDefaultTheNewOutbound(t *testing.T) {
	available := []string{"vless-reality", "vless-grpc", "tuic", "hysteria2", "anytls", "naive"}

	got := EnabledProxyTags(available, theShippedFive, nil)
	if !reflect.DeepEqual(got, available) {
		t.Errorf("enabled = %v, want everything the profile offers %v", got, available)
	}
}

// And the other half: a choice someone actually made survives a new outbound
// appearing. This is the case the rule must not swallow.
func TestEnabledProxyTagsKeepsAHandPickedSubset(t *testing.T) {
	available := []string{"vless-reality", "vless-grpc", "tuic", "hysteria2", "anytls", "naive"}

	got := EnabledProxyTags(available, []string{"tuic", "hysteria2"}, nil)
	want := []string{"tuic", "hysteria2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("enabled = %v, want the user's own %v", got, want)
	}
}

// A refusal recorded against those same five stays a refusal.
func TestEnabledProxyTagsKeepsARefusalBesideTheFive(t *testing.T) {
	available := []string{"vless-reality", "vless-grpc", "tuic", "hysteria2", "anytls", "naive"}

	got := EnabledProxyTags(available, theShippedFive, []string{"naive"})
	want := []string{"vless-reality", "vless-grpc", "tuic", "hysteria2", "anytls"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("enabled = %v, want naive left off %v", got, want)
	}
}

// On a profile that still offers exactly those five, the rule changes nothing —
// which is why it is safe to apply to a configuration the user did write by
// hand back when five was all there was.
func TestEnabledProxyTagsUnchangedWhileTheProfileIsTheSame(t *testing.T) {
	got := EnabledProxyTags(theShippedFive, theShippedFive, nil)
	if !reflect.DeepEqual(got, theShippedFive) {
		t.Errorf("enabled = %v, want %v", got, theShippedFive)
	}
}

// The recogniser is deliberately narrow: it names one historical file, not a
// set of protocols the client knows about. Growing it would start reclassifying
// real choices as things nobody chose.
func TestShippedDefaultListIsTheHistoricalOne(t *testing.T) {
	if !reflect.DeepEqual(shippedDefaultOutbounds, theShippedFive) {
		t.Errorf("shippedDefaultOutbounds = %v, want the list the .pkg shipped %v", shippedDefaultOutbounds, theShippedFive)
	}
}
