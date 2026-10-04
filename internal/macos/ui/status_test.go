package ui

import (
	"reflect"
	"testing"
)

func TestStatusBarTitle(t *testing.T) {
	tests := []struct {
		count int
		want  string
	}{
		{-1, "○"},
		{0, "○"},
		{1, "①"},
		{9, "⑨"},
		{20, "⑳"},
		{21, "20+"},
		{1000, "20+"},
	}
	for _, tc := range tests {
		if got := statusBarTitle(tc.count); got != tc.want {
			t.Errorf("statusBarTitle(%d) = %q, want %q", tc.count, got, tc.want)
		}
	}
}

func TestTrimLeadingStatusGlyph(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"   ", ""},
		{"✓ intra", "intra"},
		{"✗ projects", "projects"},
		{"  ✓   jorbique-prod  ", "jorbique-prod"},
		{"intra", "intra"},
		{"  plain name  ", "plain name"},
		{"✓✓ double", "✓ double"}, // only one leading glyph is stripped
	}
	for _, tc := range tests {
		if got := trimLeadingStatusGlyph(tc.in); got != tc.want {
			t.Errorf("trimLeadingStatusGlyph(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestConnectivityHasHTTPFail(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"HTTP OK", false},
		{"http ok", false},
		{"HTTP FAIL", true},
		{"http fail", true},
		{"ICMP ok, HTTP FAIL", true},
		{"reachable", false},
		{"FAIL but no protocol", false},
		{"HTTP OK FAIL", false}, // contains "HTTP OK" so excluded
	}
	for _, tc := range tests {
		if got := connectivityHasHTTPFail(tc.in); got != tc.want {
			t.Errorf("connectivityHasHTTPFail(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseRunningVPNs(t *testing.T) {
	out := "" +
		"\x1b[32m✓ intra\x1b[0m connected\n" +
		"✗ projects stopped\n" +
		"✓ jorbique-prod connected\n" +
		"some noise line\n"
	got := parseRunningVPNs(out)
	want := []string{"intra", "jorbique-prod"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseRunningVPNs = %v, want %v", got, want)
	}
}

func TestParseRunningVPNsEmpty(t *testing.T) {
	if got := parseRunningVPNs("nothing here\n"); len(got) != 0 {
		t.Errorf("parseRunningVPNs = %v, want empty", got)
	}
}

func TestParseMenuStatus(t *testing.T) {
	out := "" +
		"✓ intra connected\n" +
		"  connectivity: HTTP OK\n" +
		"✗ projects stopped\n" +
		"✓ jorbique-prod connected\n" +
		"  connectivity: HTTP FAIL\n" +
		"sudoers: binary in sudoers OK\n" +
		"VPN autostart: enabled\n" +
		"App autostart: disabled\n"

	got := parseMenuStatus(out)

	wantLines := []VPNLine{
		{Name: "intra", Connected: true, Connectivity: "HTTP OK"},
		{Name: "projects", Connected: false},
		{Name: "jorbique-prod", Connected: true, Connectivity: "HTTP FAIL"},
	}
	if !reflect.DeepEqual(got.VPNLines, wantLines) {
		t.Errorf("VPNLines =\n %+v\nwant\n %+v", got.VPNLines, wantLines)
	}
	if got.SudoersLine != "sudoers: binary in sudoers OK" {
		t.Errorf("SudoersLine = %q", got.SudoersLine)
	}
	if got.AutostartLine != "VPN autostart: enabled" {
		t.Errorf("AutostartLine = %q", got.AutostartLine)
	}
	if got.BarAutostartLine != "App autostart: disabled" {
		t.Errorf("BarAutostartLine = %q", got.BarAutostartLine)
	}
}

func TestParseMenuStatusDefaults(t *testing.T) {
	got := parseMenuStatus("")
	if got.SudoersLine != defaultSudoersLine ||
		got.AutostartLine != defaultAutostartLine ||
		got.BarAutostartLine != defaultBarAutostartLine {
		t.Errorf("defaults not applied: %+v", got)
	}
	if len(got.VPNLines) != 0 {
		t.Errorf("VPNLines = %v, want empty", got.VPNLines)
	}
}

func TestParseMenuStatusAltHeaderPrefixes(t *testing.T) {
	// "autostart:" and "Bar autostart:" are the legacy prefixes also accepted.
	out := "autostart: on\nBar autostart: off\n"
	got := parseMenuStatus(out)
	if got.AutostartLine != "autostart: on" {
		t.Errorf("AutostartLine = %q", got.AutostartLine)
	}
	if got.BarAutostartLine != "Bar autostart: off" {
		t.Errorf("BarAutostartLine = %q", got.BarAutostartLine)
	}
}

// Connectivity attaches only to a connected line of the same name, and only the
// most recent one — a stopped line of that name must not absorb it.
func TestParseMenuStatusConnectivityBindsToConnected(t *testing.T) {
	out := "" +
		"✓ intra connected\n" +
		"✗ intra stopped\n" +
		"  connectivity: HTTP OK\n"
	got := parseMenuStatus(out)
	want := []VPNLine{
		{Name: "intra", Connected: true, Connectivity: "HTTP OK"},
		{Name: "intra", Connected: false},
	}
	if !reflect.DeepEqual(got.VPNLines, want) {
		t.Errorf("VPNLines =\n %+v\nwant\n %+v", got.VPNLines, want)
	}
}

// `status` emits the heal gates only when something failed; the healer reads
// them to decide whether restarting can possibly help.
func TestParseMenuStatusHealGates(t *testing.T) {
	out := "✓ intra connected\n" +
		"    connectivity [wplan.office.lan]: http FAIL\n" +
		"internet: unreachable\n" +
		"user session: active\n"
	got := parseMenuStatus(out)
	if got.InternetOK == nil || *got.InternetOK {
		t.Errorf("InternetOK = %v, want false", got.InternetOK)
	}
	if got.UserSessionOK == nil || !*got.UserSessionOK {
		t.Errorf("UserSessionOK = %v, want true", got.UserSessionOK)
	}
	// The gate lines must not be mistaken for VPN lines.
	if len(got.VPNLines) != 1 || got.VPNLines[0].Name != "intra" {
		t.Errorf("VPNLines = %+v, want just intra", got.VPNLines)
	}
	if got.VPNLines[0].Connectivity != "http FAIL" {
		t.Errorf("connectivity = %q", got.VPNLines[0].Connectivity)
	}
}

// A healthy run prints no gate lines; absent gates must not block healing.
func TestParseMenuStatusNoHealGates(t *testing.T) {
	got := parseMenuStatus("✓ intra connected\n    connectivity [wplan.office.lan]: http OK\n")
	if got.InternetOK != nil || got.UserSessionOK != nil {
		t.Errorf("gates = %v/%v, want both nil", got.InternetOK, got.UserSessionOK)
	}
	if reason := healBlockedReason(got); reason != "" {
		t.Errorf("healBlockedReason = %q, want none", reason)
	}
}

func TestHealBlockedReason(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		name string
		ms   MenuStatus
		want string
	}{
		{"nothing reported (older CLI)", MenuStatus{}, ""},
		{"all clear", MenuStatus{InternetOK: &yes, UserSessionOK: &yes}, ""},
		{"network down", MenuStatus{InternetOK: &no, UserSessionOK: &yes}, "no working internet"},
		{"machine locked", MenuStatus{InternetOK: &yes, UserSessionOK: &no}, "no active user session"},
		// Network down wins: it is the one the user can least act on.
		{"both", MenuStatus{InternetOK: &no, UserSessionOK: &no}, "no working internet"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := healBlockedReason(tt.ms); got != tt.want {
				t.Errorf("healBlockedReason = %q, want %q", got, tt.want)
			}
		})
	}
}

// The line the CLI prints the moment a tunnel is up. The menu bar reads it off
// the running start to retire that VPN from the header, instead of waiting for
// the whole batch to finish.
func TestConnectedNameFromLine(t *testing.T) {
	green := "\x1b[32m✓ intra connected\x1b[0m"
	if name, ok := connectedNameFromLine(green); !ok || name != "intra" {
		t.Errorf("connectedNameFromLine(%q) = %q, %v", green, name, ok)
	}
	if name, ok := connectedNameFromLine("✓ jorbique-prod connected"); !ok || name != "jorbique-prod" {
		t.Errorf("name = %q, ok = %v", name, ok)
	}
	for _, line := range []string{
		"",
		"Connectivity checks:",
		"sing-box did not start within 30s. Last 25 lines of log:",
		"✗ projects stopped",
	} {
		if name, ok := connectedNameFromLine(line); ok {
			t.Errorf("connectedNameFromLine(%q) read a VPN named %q", line, name)
		}
	}
}
