package semver

import (
	"strconv"
	"strings"
)

// Compare returns 1 if a > b, 0 if equal, -1 if a < b.
// Expects dotted versions like "1.13.4". Non-numeric suffixes on a segment are ignored (e.g. "1.2.3-rc" → 1.2.3).
func Compare(a, b string) int {
	pa := parse(a)
	pb := parse(b)
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x > y {
			return 1
		}
		if x < y {
			return -1
		}
	}
	return 0
}

func parse(s string) []int {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		n, err := strconv.Atoi(p)
		if err != nil {
			out = append(out, 0)
			continue
		}
		out = append(out, n)
	}
	return out
}
