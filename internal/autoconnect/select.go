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
	// Failed lists the protocols whose check ran and did not pass. A protocol
	// in neither list was never checked: Naive won first, or the scan ran out
	// of time before reaching it.
	Failed []string
	Naive  bool
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
	var failed []string
	test := func(tag string) (Measurement, error) {
		if err := ctx.Err(); err != nil {
			return Measurement{}, err
		}
		if logf != nil {
			logf("Checking connection: " + tag)
		}
		m, err := probe(ctx, tag)
		if err != nil && ctx.Err() == nil {
			failed = append(failed, tag)
		}
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
			return Result{Working: []Measurement{m}, Failed: failed, Naive: true}, nil
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
	// A user's cancel always wins. The scan deadline does not: protocols that
	// already passed are real, so a slow tail must not throw them away.
	if err := ctx.Err(); err != nil && (errors.Is(err, context.Canceled) || len(result.Working) == 0) {
		return Result{}, err
	}
	if len(result.Working) == 0 {
		return Result{}, ErrUnavailable
	}
	result.Failed = failed
	sort.SliceStable(result.Working, func(i, j int) bool { return result.Working[i].Elapsed < result.Working[j].Elapsed })
	return result, nil
}

// FailoverTolerance keeps a urltest on the measured winner while it answers:
// a rival must be this many ms faster to take over, which a live winner never
// is. When the winner stops answering it loses its history and the next check
// moves to the fastest verified alternative. Below 65535 - real delays, so the
// engine's uint16 sum cannot wrap.
const FailoverTolerance = 30000

// ConfigureGroups keeps only the verified choices, ordered by the transfer
// test. A urltest starts on the winner (its first member) and only fails over
// when that stops answering; a selector offers the urltest first, so a
// restored manual choice cannot pin a protocol that later dies.
func (r Result) ConfigureGroups(root *jsonorder.Map) {
	members := make([]any, 0, len(r.Working))
	for _, item := range r.Working {
		members = append(members, item.Tag)
	}
	var urltests []any
	for _, group := range groups(root) {
		if group.GetString("type") == "urltest" {
			urltests = append(urltests, group.GetString("tag"))
		}
	}
	for _, group := range groups(root) {
		if group.GetString("type") == "urltest" {
			group.Set("outbounds", members)
			group.Set("interval", "1m")
			group.Set("tolerance", FailoverTolerance)
			group.Set("interrupt_exist_connections", true)
			continue
		}
		group.Set("outbounds", append(append([]any{}, urltests...), members...))
		if len(urltests) > 0 {
			group.Set("default", urltests[0])
		} else {
			group.Set("default", r.Winner())
		}
	}
}

// pinGroups turns every group into a selector fixed on tag: a probe measures
// one protocol, with no background tests competing with the transfer.
func pinGroups(root *jsonorder.Map, tag string) {
	for _, group := range groups(root) {
		group.Set("type", "selector")
		for _, key := range []string{"url", "interval", "tolerance", "idle_timeout", "interrupt_exist_connections"} {
			group.Delete(key)
		}
		group.Set("outbounds", []any{tag})
		group.Set("default", tag)
	}
}

func groups(root *jsonorder.Map) []*jsonorder.Map {
	raw, _ := root.Get("outbounds")
	obs, _ := raw.([]any)
	var out []*jsonorder.Map
	for _, raw := range obs {
		group, ok := raw.(*jsonorder.Map)
		if !ok {
			continue
		}
		if typ := group.GetString("type"); typ == "selector" || typ == "urltest" {
			out = append(out, group)
		}
	}
	return out
}
