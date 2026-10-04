package singboxcfg

import (
	"slices"
	"strings"
)

// NaiveProxy travels in the same profile as every other protocol, but it is a
// mode of its own rather than one more toggle: it is the heaviest of them, and
// in the failover group it would run beside all the rest. So the NaiveProxy
// mode builds from the naive outbounds alone, and the protocols mode never
// starts them, whatever the toggles say.

// naiveType is the outbound type of NaiveProxy.
const naiveType = "naive"

// NaiveTags lists the profile's NaiveProxy outbounds, by type: the server names
// its tags.
func NaiveTags(outbounds []any) []string {
	var tags []string
	for _, m := range outboundMaps(outbounds) {
		if strings.EqualFold(stringField(m, "type"), naiveType) {
			if tag := stringField(m, "tag"); tag != "" {
				tags = append(tags, tag)
			}
		}
	}
	return tags
}

// ProtocolTags is ProxyTags without the NaiveProxy outbounds: the protocols a
// user switches on and off in the protocols mode, and so what the settings
// window draws a toggle for.
func ProtocolTags(outbounds []any, entries []string) []string {
	naive := NaiveTags(outbounds)
	var tags []string
	for _, tag := range ProxyTags(outbounds, entries) {
		if !slices.Contains(naive, tag) {
			tags = append(tags, tag)
		}
	}
	return tags
}

// ModeEnabled is the set of answers a build hands FilterOutbounds in the chosen
// mode. The NaiveProxy mode keeps the naive outbounds and nothing else, which
// leaves "auto" with naive alone; the protocols mode keeps the user's own
// answers with naive always off. available is ProxyTags of the same profile.
func ModeEnabled(outbounds []any, available []string, enabled map[string]bool, naiveMode bool) map[string]bool {
	naive := map[string]bool{}
	for _, tag := range NaiveTags(outbounds) {
		naive[tag] = true
	}
	out := make(map[string]bool, len(available)+len(enabled))
	if naiveMode {
		for _, tag := range available {
			out[tag] = naive[tag]
		}
		return out
	}
	for tag, on := range enabled {
		out[tag] = on
	}
	for tag := range naive {
		out[tag] = false
	}
	return out
}
