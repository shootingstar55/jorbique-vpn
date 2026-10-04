package jsonorder

import (
	"strings"
	"testing"
)

// The profile is written by hand on the server; the config the client runs
// should read like it, field for field, in the same order.
func TestOrderSurvivesAReadAndWrite(t *testing.T) {
	const src = `{"type":"snell","tag":"snell","server":"4.231.110.10","server_port":49211,` +
		`"version":6,"psk":"a<b>c&d","userkey":"","reuse":true,"mode":"default","udp_fragment":true}`

	m := New()
	if err := Unmarshal([]byte(src), m); err != nil {
		t.Fatal(err)
	}
	// Rewriting a field must not move it: the credential goes in where the
	// placeholder was.
	m.Set("userkey", "7998568d-7ae4-4f65-a4cb-b20e78af6a87")

	out, err := m.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"type":"snell","tag":"snell","server":"4.231.110.10","server_port":49211,` +
		`"version":6,"psk":"a<b>c&d","userkey":"7998568d-7ae4-4f65-a4cb-b20e78af6a87",` +
		`"reuse":true,"mode":"default","udp_fragment":true}`
	if string(out) != want {
		t.Errorf("round trip:\n got %s\nwant %s", out, want)
	}
}

// A pre-shared key is full of the characters the standard encoder escapes. It
// stays legible here, and an integer port stays an integer.
func TestStringsAndNumbersComeBackUnchanged(t *testing.T) {
	const psk = `,Q)],[nmD+TJSk3Cpyr4+y@4*3|<ImJl-Cd2I:s}aBC][#~o9tE=Jl0J$:]>:M>ZlWm+&x`
	m := New()
	if err := Unmarshal([]byte(`{"psk":`+quote(psk)+`,"server_port":49211}`), m); err != nil {
		t.Fatal(err)
	}

	out, err := m.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, escaped := range []string{`\u003c`, `\u003e`, `\u0026`} {
		if strings.Contains(string(out), escaped) {
			t.Errorf("the encoder escaped %s, which the profile had in plain: %s", escaped, out)
		}
	}
	if !strings.Contains(string(out), `"server_port":49211`) {
		t.Errorf("the port did not survive as an integer: %s", out)
	}
}

// Indented output is what lands on disk, and it follows the same order.
func TestMarshalIndentKeepsOrder(t *testing.T) {
	m := New()
	if err := Unmarshal([]byte(`{"b":1,"a":{"d":2,"c":[3,4]}}`), m); err != nil {
		t.Fatal(err)
	}

	out, err := MarshalIndent(m)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"b\": 1,\n  \"a\": {\n    \"d\": 2,\n    \"c\": [\n      3,\n      4\n    ]\n  }\n}\n"
	if string(out) != want {
		t.Errorf("indented output:\n got %q\nwant %q", out, want)
	}
}

func quote(s string) string {
	b, _ := encodeScalar(s)
	return string(b)
}
