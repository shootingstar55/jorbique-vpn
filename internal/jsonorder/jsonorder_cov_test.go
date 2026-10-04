package jsonorder

import "testing"

// Malformed input nested inside an object or array is an error, wherever in
// the tree it sits.
func TestUnmarshalRejectsNestedMalformedJSON(t *testing.T) {
	for _, src := range []string{
		`{"a":{1:2}}`,   // a nested key that is not a string
		`{"a":{"b":}}`,  // a nested value missing
		`{"a":{"b":1]}`, // a nested object closed as an array
		`{"a":[:]}`,     // a bad value inside an array
		`{"a":[1}}`,     // an array closed as an object
	} {
		t.Run(src, func(t *testing.T) {
			if err := Unmarshal([]byte(src), New()); err == nil {
				t.Errorf("Unmarshal(%s) succeeded", src)
			}
		})
	}
}
