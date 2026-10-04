package ansiutil

import "testing"

func TestColoursWrapAndStripBack(t *testing.T) {
	for _, tc := range []struct {
		name string
		fn   func(string) string
		want string
	}{
		{"green", Green, "\x1b[32mok\x1b[0m"},
		{"red", Red, "\x1b[31mok\x1b[0m"},
		{"yellow", Yellow, "\x1b[33mok\x1b[0m"},
	} {
		got := tc.fn("ok")
		if got != tc.want {
			t.Errorf("%s(ok) = %q, want %q", tc.name, got, tc.want)
		}
		if s := Strip(got); s != "ok" {
			t.Errorf("Strip(%s(ok)) = %q, want ok", tc.name, s)
		}
	}
}

func TestStrip(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"plain", "plain"},
		{"\x1b[1;31mbold red\x1b[0m done", "bold red done"},
		{"ünï\x1b[32mcødé\x1b[0m", "ünïcødé"},
		// ESC not followed by '[' is not a colour sequence and is kept.
		{"a\x1bb", "a\x1bb"},
		{"tail\x1b", "tail\x1b"},
		// An unterminated sequence still drops ESC [ and its parameters.
		{"x\x1b[12Ky", "xKy"},
		{"end\x1b[", "end"},
	} {
		if got := Strip(tc.in); got != tc.want {
			t.Errorf("Strip(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
