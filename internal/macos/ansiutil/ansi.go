package ansiutil

import "strings"

const (
	reset  = "\x1b[0m"
	green  = "\x1b[32m"
	red    = "\x1b[31m"
	yellow = "\x1b[33m"
)

func Green(s string) string  { return green + s + reset }
func Red(s string) string    { return red + s + reset }
func Yellow(s string) string { return yellow + s + reset }

func Strip(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		if j, ok := skipEscape(runes, i); ok {
			i = j
			continue
		}
		b.WriteRune(runes[i])
		i++
	}
	return b.String()
}

// skipEscape reports whether an ESC [ sequence starts at i and, if so, the
// index just past its digits, semicolons and a closing 'm'.
func skipEscape(runes []rune, i int) (int, bool) {
	j := i + 1
	if runes[i] != 0x1B || j >= len(runes) || runes[j] != '[' {
		return 0, false
	}
	j++
	for j < len(runes) && (isDigit(runes[j]) || runes[j] == ';') {
		j++
	}
	if j < len(runes) && runes[j] == 'm' {
		j++
	}
	return j, true
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }
