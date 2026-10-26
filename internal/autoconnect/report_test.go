package autoconnect

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestReportListsEveryProtocolWithItsOutcome(t *testing.T) {
	result, err := Select(context.Background(), profile(t), func(_ context.Context, tag string) (Measurement, error) {
		switch tag {
		case "slow":
			return Measurement{Elapsed: 4 * time.Second, Latency: 90 * time.Millisecond, Bytes: PayloadBytes}, nil
		case "fast":
			return Measurement{Elapsed: time.Second, Latency: 20 * time.Millisecond, Bytes: PayloadBytes}, nil
		default:
			return Measurement{}, errors.New("blocked")
		}
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	report := NewReport(profile(t), result)
	if report.Active != "fast" || report.Naive {
		t.Fatalf("%+v", report)
	}
	states := map[string]string{}
	var order []string
	for _, row := range report.Protocols {
		states[row.Tag] = row.State
		order = append(order, row.Tag)
		if row.State == StateActive && (row.LatencyMs != 20 || row.SpeedKiBs <= 0) {
			t.Fatalf("active row without its numbers: %+v", row)
		}
	}
	want := map[string]string{"slow": StateStandby, "preferred-renamed": StateFailed, "fast": StateActive, "dead": StateFailed}
	if !reflect.DeepEqual(states, want) {
		t.Fatalf("states=%v", states)
	}
	// The profile's order (the urltest first), not the ranking: the list does
	// not reshuffle per connect.
	if !reflect.DeepEqual(order, []string{"slow", "fast", "dead", "preferred-renamed"}) {
		t.Fatalf("order=%v", order)
	}
}

func TestReportMarksProtocolsANaiveWinSkipped(t *testing.T) {
	result, err := Select(context.Background(), profile(t), func(context.Context, string) (Measurement, error) {
		return Measurement{Elapsed: time.Second, Bytes: PayloadBytes}, nil
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	report := NewReport(profile(t), result)
	if report.Active != "preferred-renamed" || !report.Naive {
		t.Fatalf("%+v", report)
	}
	for _, row := range report.Protocols {
		if row.Tag != "preferred-renamed" && row.State != StateSkipped {
			t.Fatalf("%s was never checked but is %s", row.Tag, row.State)
		}
	}
}

func TestReportRoundTripsThroughTheRunDir(t *testing.T) {
	dir := t.TempDir()
	if _, ok := ReadReport(dir); ok {
		t.Fatal("report before any connect")
	}
	want := Report{Active: "fast", Protocols: []ProtocolStatus{{Tag: "fast", State: StateActive, LatencyMs: 5}}}
	WriteReport(dir, want)
	got, ok := ReadReport(dir)
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
	RemoveReport(dir)
	if _, ok := ReadReport(dir); ok {
		t.Fatal("report survived RemoveReport")
	}
}
