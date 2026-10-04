package core

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestSingboxDisabledOutboundsJSONRoundTrip(t *testing.T) {
	for _, disabled := range [][]string{nil, {}, {"tuic"}} {
		original := SingboxSection{EnabledOutbounds: []string{"tuic"}, DisabledOutbounds: disabled}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		var restored SingboxSection
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(restored, original) {
			t.Errorf("round trip via %s: got %#v, want %#v", data, restored, original)
		}
	}
}
