package semver

import (
	"strconv"
	"strings"
)

// Compare returns 1 if a > b, 0 if equal, -1 if a < b.
// Expects dotted versions like "1.13.4", optionally with a pre-release
// ("1.15.0-alpha.10"). A leading "v" and "+build" metadata are ignored, a
// non-numeric segment counts as 0, and missing segments as 0 ("1.2" == "1.2.0").
// A release sorts above its pre-releases, and pre-releases compare part by
// part, numbers numerically, so alpha.10 > alpha.9 and rc > beta > alpha.
// The update check depends on this: with the pre-release dropped, a client on
// 1.15.0-alpha.10 never moved to the next alpha or to 1.15.0.
func Compare(a, b string) int {
	coreA, preA := split(a)
	coreB, preB := split(b)
	if c := compareNumbers(parse(coreA), parse(coreB)); c != 0 {
		return c
	}
	switch {
	case preA == preB:
		return 0
	case preA == "":
		return 1
	case preB == "":
		return -1
	}
	return comparePre(preA, preB)
}

// split separates the dotted core from the pre-release, dropping "v" and build metadata.
func split(s string) (core, pre string) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	s, _, _ = strings.Cut(s, "+")
	core, pre, _ = strings.Cut(s, "-")
	return core, pre
}

func compareNumbers(pa, pb []int) int {
	n := max(len(pa), len(pb))
	for i := 0; i < n; i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			if x > y {
				return 1
			}
			return -1
		}
	}
	return 0
}

// comparePre orders pre-release parts the semver way: numbers numerically and
// below words, words lexically, and a longer list above its own prefix.
func comparePre(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		x, errX := strconv.Atoi(pa[i])
		y, errY := strconv.Atoi(pb[i])
		switch {
		case errX == nil && errY == nil:
			if c := compareNumbers([]int{x}, []int{y}); c != 0 {
				return c
			}
		case errX == nil:
			return -1
		case errY == nil:
			return 1
		default:
			if c := strings.Compare(pa[i], pb[i]); c != 0 {
				return c
			}
		}
	}
	return compareNumbers([]int{len(pa)}, []int{len(pb)})
}

func parse(s string) []int {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			n = 0
		}
		out = append(out, n)
	}
	return out
}
