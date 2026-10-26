// Package autoconnect tests real HTTPS transfers through isolated sing-box
// proxies before a tunnel changes the machine's routes or DNS.
package autoconnect

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"jorbiquevpn/internal/jsonorder"
	"jorbiquevpn/internal/singboxcfg"
)

var ErrUnavailable = errors.New("No working VPN connection found. Check your internet connection and UUID, then try again")

type Measurement struct {
	Tag     string
	Elapsed time.Duration
	Latency time.Duration
	Bytes   int64
}

type Result struct {
	Working []Measurement
	Naive   bool
}

func (r Result) Winner() string {
	if len(r.Working) == 0 {
		return ""
	}
	return r.Working[0].Tag
}

// Enabled explicitly refuses every untested/failed candidate. No old preference
// or saved selector choice is used in an automatic connection.
func (r Result) Enabled(profile *jsonorder.Map) map[string]bool {
	enabled := map[string]bool{}
	for _, tag := range Candidates(profile) {
		enabled[tag] = false
	}
	for _, item := range r.Working {
		enabled[item.Tag] = true
	}
	return enabled
}

func Candidates(profile *jsonorder.Map) []string {
	raw, _ := profile.Get("outbounds")
	obs, _ := raw.([]any)
	return singboxcfg.ProxyTags(obs, singboxcfg.RouteEntries(profile))
}

type Probe func(context.Context, string) (Measurement, error)

// Select always finishes the Naive phase first. A successful Naive transfer
// short-circuits all other protocols; otherwise every remaining candidate is
// tested afresh and ranked by the time to complete the same HTTPS payload.
func Select(ctx context.Context, profile *jsonorder.Map, probe Probe, logf func(string), report func(Progress)) (Result, error) {
	raw, _ := profile.Get("outbounds")
	obs, _ := raw.([]any)
	naive := map[string]bool{}
	for _, tag := range singboxcfg.NaiveTags(obs) {
		naive[tag] = true
	}
	available := Candidates(profile)
	preferredCount := 0
	for _, tag := range available {
		if naive[tag] {
			preferredCount++
		}
	}
	progress := Progress{Phase: "preferred", Total: preferredCount}
	update := func() {
		if report != nil {
			report(progress)
		}
	}
	update()
	test := func(tag string) (Measurement, error) {
		if err := ctx.Err(); err != nil {
			return Measurement{}, err
		}
		if logf != nil {
			logf("Checking connection: " + tag)
		}
		m, err := probe(ctx, tag)
		progress.Completed++
		update()
		m.Tag = tag
		if logf != nil {
			if err != nil {
				logf("Connection unavailable: " + tag)
			} else {
				logf(fmt.Sprintf("Connection ready: %s (%d ms, %.0f KiB/s)", tag, m.Latency.Milliseconds(), float64(m.Bytes)/1024/m.Elapsed.Seconds()))
			}
		}
		return m, err
	}
	for _, tag := range available {
		if !naive[tag] {
			continue
		}
		if m, err := test(tag); err == nil {
			return Result{Working: []Measurement{m}, Naive: true}, nil
		}
	}
	result := Result{}
	progress = Progress{Phase: "alternatives", Total: len(available) - preferredCount}
	update()
	for _, tag := range available {
		if naive[tag] {
			continue
		}
		if m, err := test(tag); err == nil {
			result.Working = append(result.Working, m)
		}
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if len(result.Working) == 0 {
		return Result{}, ErrUnavailable
	}
	sort.SliceStable(result.Working, func(i, j int) bool { return result.Working[i].Elapsed < result.Working[j].Elapsed })
	return result, nil
}

// ConfigureGroups keeps only the verified choices, ordered by the transfer
// test. Selectors start on the winner; a background latency-only URLTest must
// not silently override the speed test or resurrect a previous manual choice.
func (r Result) ConfigureGroups(root *jsonorder.Map) {
	raw, _ := root.Get("outbounds")
	obs, _ := raw.([]any)
	for _, raw := range obs {
		group, ok := raw.(*jsonorder.Map)
		if !ok {
			continue
		}
		if typ := group.GetString("type"); typ != "selector" && typ != "urltest" {
			continue
		}
		group.Set("type", "selector")
		for _, key := range []string{"url", "interval", "tolerance", "idle_timeout"} {
			group.Delete(key)
		}
		members := make([]any, 0, len(r.Working))
		for _, item := range r.Working {
			members = append(members, item.Tag)
		}
		group.Set("outbounds", members)
		group.Set("default", r.Winner())
	}
}
