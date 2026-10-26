package ui

import (
	"reflect"
	"testing"
	"time"
)

// The alarm must survive a blip and still fire on a real outage. These are the
// two failures the old "red on the first failed probe" behaviour got wrong.
func TestConnFailTrackerNeedsCorroboration(t *testing.T) {
	var tr connFailTracker
	t0 := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)

	if got := tr.observe([]string{"projects"}, t0); got != nil {
		t.Errorf("first failure = %v, want nothing shown yet", got)
	}
	// A second failure too soon after the first is still not enough: right after
	// an action the bar polls every 2s, and a settling tunnel would trip it.
	if got := tr.observe([]string{"projects"}, t0.Add(2*time.Second)); got != nil {
		t.Errorf("second failure 2s later = %v, want nothing shown yet", got)
	}
	if got := tr.observe([]string{"projects"}, t0.Add(showFailMinSpan)); !reflect.DeepEqual(got, []string{"projects"}) {
		t.Errorf("failure past the span = %v, want it shown", got)
	}
}

func TestConnFailTrackerRecoveryClearsAtOnce(t *testing.T) {
	var tr connFailTracker
	t0 := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	tr.observe([]string{"projects"}, t0)
	if got := tr.observe([]string{"projects"}, t0.Add(showFailMinSpan)); len(got) != 1 {
		t.Fatalf("expected the alarm to be up, got %v", got)
	}
	// One good check clears it — slow to accuse, quick to forgive.
	if got := tr.observe(nil, t0.Add(showFailMinSpan+time.Second)); got != nil {
		t.Errorf("after recovery = %v, want nothing", got)
	}
	// And the streak starts over, so the next failure has to earn it again.
	if got := tr.observe([]string{"projects"}, t0.Add(2*time.Minute)); got != nil {
		t.Errorf("first failure after recovery = %v, want nothing shown yet", got)
	}
}

// Each VPN is judged on its own: one failing tunnel must not raise the alarm for
// another that has only just started failing.
func TestConnFailTrackerPerVPN(t *testing.T) {
	var tr connFailTracker
	t0 := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	tr.observe([]string{"intra"}, t0)
	got := tr.observe([]string{"intra", "projects"}, t0.Add(showFailMinSpan))
	if !reflect.DeepEqual(got, []string{"intra"}) {
		t.Errorf("confirmed = %v, want only intra", got)
	}
}

// The reason is what turns "the VPN is broken" into a report someone can act on,
// so parsing it out of the status line is worth pinning down.
func TestConnectivityReasonAndLabel(t *testing.T) {
	tests := []struct {
		conn   string
		reason string
		label  string
	}{
		{"http FAIL (dns)", "dns", "DNS"},
		{"http FAIL (timeout)", "timeout", "timeout"},
		{"http FAIL (http 503)", "http 503", "HTTP 503"},
		{"http FAIL (refused)", "refused", "refused"},
		{"http FAIL (dns) — TLS handshake failed", "dns", "DNS"},
		{"http FAIL", "", "нет связи"},
		{"http OK", "", "нет связи"},
		{"", "", "нет связи"},
	}
	for _, tt := range tests {
		if got := connectivityReason(tt.conn); got != tt.reason {
			t.Errorf("connectivityReason(%q) = %q, want %q", tt.conn, got, tt.reason)
		}
		if got := connFailLabel(connectivityReason(tt.conn)); got != tt.label {
			t.Errorf("connFailLabel(%q) = %q, want %q", tt.conn, got, tt.label)
		}
	}
}

// The header has to name the subsystem: a DNS failure reported as "http" is what
// sent a whole investigation after the web service.
func TestFailureHeaderNamesTheReason(t *testing.T) {
	ms := MenuStatus{VPNLines: []VPNLine{
		{Name: "projects", Connected: true, Connectivity: "http FAIL (dns)"},
		{Name: "intra", Connected: true, Connectivity: "http FAIL (http 502)"},
	}}
	if got := failureHeader(ms, []string{"projects"}); got != "⚠ projects: DNS" {
		t.Errorf("single failure header = %q", got)
	}
	if got := failureHeader(ms, []string{"intra"}); got != "⚠ intra: HTTP 502" {
		t.Errorf("http failure header = %q", got)
	}
	// Several at once nearly always share one cause, and the reasons would not
	// fit in the menu bar.
	if got := failureHeader(ms, []string{"projects", "intra"}); got != "⚠ Ошибка: projects, intra" {
		t.Errorf("multi failure header = %q", got)
	}
}
