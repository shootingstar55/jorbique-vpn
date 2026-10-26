package autoconnect

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"jorbiquevpn/internal/jsonorder"
)

const selectionProfile = `{"outbounds":[
 {"type":"vless","tag":"slow","uuid":""},
 {"type":"naive","tag":"preferred-renamed","username":"","password":"shared"},
 {"type":"vless","tag":"fast","uuid":""},
 {"type":"vless","tag":"dead","uuid":""},
 {"type":"urltest","tag":"auto","outbounds":["slow","fast","dead"]},
 {"type":"selector","tag":"proxy","outbounds":["auto"],"default":"auto"},
 {"type":"direct","tag":"direct"}],"route":{"final":"proxy"}}`

func profile(t *testing.T) *jsonorder.Map {
	t.Helper()
	root := jsonorder.New()
	if err := jsonorder.Unmarshal([]byte(selectionProfile), root); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestNaiveAlwaysFirstAndExclusive(t *testing.T) {
	var calls []string
	var progress []Progress
	result, err := Select(context.Background(), profile(t), func(_ context.Context, tag string) (Measurement, error) {
		calls = append(calls, tag)
		return Measurement{Elapsed: time.Second, Bytes: PayloadBytes}, nil
	}, nil, func(p Progress) { progress = append(progress, p) })
	if err != nil || !result.Naive || result.Winner() != "preferred-renamed" || !reflect.DeepEqual(calls, []string{"preferred-renamed"}) {
		t.Fatalf("selection=%+v calls=%v err=%v", result, calls, err)
	}
	if progress[len(progress)-1] != (Progress{Phase: "preferred", Completed: 1, Total: 1}) {
		t.Fatalf("progress=%v", progress)
	}
	enabled := result.Enabled(profile(t))
	if enabled["slow"] || enabled["fast"] || !enabled["preferred-renamed"] {
		t.Fatal(enabled)
	}
}

func TestFallbackRanksTransfersAndRetestsOnEveryConnect(t *testing.T) {
	var calls []string
	var progress []Progress
	for range 2 {
		result, err := Select(context.Background(), profile(t), func(_ context.Context, tag string) (Measurement, error) {
			calls = append(calls, tag)
			switch tag {
			case "slow":
				return Measurement{Elapsed: 3 * time.Second, Latency: time.Millisecond, Bytes: PayloadBytes}, nil
			case "fast":
				return Measurement{Elapsed: time.Second, Latency: 20 * time.Millisecond, Bytes: PayloadBytes}, nil
			default:
				return Measurement{}, errors.New("blocked")
			}
		}, nil, func(p Progress) { progress = append(progress, p) })
		if err != nil || result.Naive || result.Winner() != "fast" || len(result.Working) != 2 {
			t.Fatalf("%+v %v", result, err)
		}
		enabled := result.Enabled(profile(t))
		if enabled["dead"] || enabled["preferred-renamed"] || !enabled["slow"] {
			t.Fatal(enabled)
		}
		root := profile(t)
		result.ConfigureGroups(root)
		raw, _ := root.Get("outbounds")
		for _, raw := range raw.([]any) {
			ob := raw.(*jsonorder.Map)
			if ob.GetString("tag") == "auto" || ob.GetString("tag") == "proxy" {
				if ob.GetString("type") != "selector" || ob.GetString("default") != "fast" {
					t.Fatal(ob)
				}
				members, _ := ob.Get("outbounds")
				if !reflect.DeepEqual(members, []any{"fast", "slow"}) {
					t.Fatal(members)
				}
			}
		}
	}
	want := []string{"preferred-renamed", "slow", "fast", "dead", "preferred-renamed", "slow", "fast", "dead"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("%v", calls)
	}
	if progress[len(progress)-1] != (Progress{Phase: "alternatives", Completed: 3, Total: 3}) {
		t.Fatal(progress)
	}
}

func TestNoWorkingConnectionAndCancellationFailClosed(t *testing.T) {
	_, err := Select(context.Background(), profile(t), func(context.Context, string) (Measurement, error) { return Measurement{}, errors.New("blocked") }, nil, nil)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	_, err = Select(ctx, profile(t), func(context.Context, string) (Measurement, error) { calls++; return Measurement{}, nil }, nil, nil)
	if !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestProbeConfigKeepsServerSudokuKeyAndOnlyBindsExplicitUUID(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	const raw = `{"inbounds":[{"type":"tun","auto_route":true}],"outbounds":[
 {"type":"sudoku","tag":"sudoku","key":"shared-server-key","http_opts":{"path":"__JORBIQUE_USER_UUID__"},"tls":{"server_name":""}},
 {"type":"vless","tag":"other","uuid":""},
 {"type":"urltest","tag":"auto","outbounds":["sudoku","other"]}],"route":{"final":"auto","rules":[{"outbound":"other"}],"rule_set":[{"url":"https://invalid.example/file"}]},"services":[{"type":"api"}],"experimental":{"cache_file":{"enabled":true}}}`
	root, err := ProbeConfig([]byte(raw), id, "sudoku", 1080, "token")
	if err != nil {
		t.Fatal(err)
	}
	if root.Has("services") || root.Has("experimental") {
		t.Fatal("probe kept API or cache")
	}
	out, _ := root.Get("outbounds")
	obs := out.([]any)
	sdk := obs[0].(*jsonorder.Map)
	if sdk.GetString("key") != "shared-server-key" {
		t.Fatal("server key changed")
	}
	h, _ := sdk.Get("http_opts")
	if h.(*jsonorder.Map).GetString("path") != id {
		t.Fatal("UUID path missing")
	}
	tls, _ := sdk.Get("tls")
	if tls.(*jsonorder.Map).GetString("server_name") != "" {
		t.Fatal("UUID leaked into TLS")
	}
	route, _ := root.Get("route")
	if route.(*jsonorder.Map).GetString("final") != "sudoku" || route.(*jsonorder.Map).Has("rule_set") {
		t.Fatal("probe can bypass requested protocol")
	}
	for _, raw := range obs {
		if raw.(*jsonorder.Map).GetString("tag") == "other" {
			t.Fatal("failed protocol survived")
		}
	}
	in, _ := root.Get("inbounds")
	ib := in.([]any)[0].(*jsonorder.Map)
	if ib.GetString("type") != "socks" || ib.GetString("listen") != "127.0.0.1" {
		t.Fatal("probe touches TUN/network")
	}
}
