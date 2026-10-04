package ui

import (
	"reflect"
	"strings"
	"testing"
)

func TestConnectivityFailures(t *testing.T) {
	ms := MenuStatus{VPNLines: []VPNLine{
		{Name: "intra", Connected: true, Connectivity: "HTTP OK"},
		{Name: "projects", Connected: true, Connectivity: "ICMP ok, HTTP FAIL"},
		{Name: "stopped-one", Connected: false, Connectivity: "HTTP FAIL"}, // not connected -> ignored
		{Name: "jorbique", Connected: true, Connectivity: "HTTP FAIL"},
	}}
	got := connectivityFailures(ms)
	want := []string{"projects", "jorbique"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("connectivityFailures = %v, want %v", got, want)
	}
}

func TestAlertBody(t *testing.T) {
	tests := []struct {
		name string
		out  string
		rc   int
		want string
	}{
		{"plain", "boom happened\n", 1, "boom happened"},
		{"ansi stripped", "\x1b[31mred error\x1b[0m\n", 2, "red error"},
		{"empty falls back to exit code", "   \n", 7, "Exit code 7"},
		{"whitespace only", "", 3, "Exit code 3"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := alertBody(tc.out, tc.rc); got != tc.want {
				t.Errorf("alertBody(%q,%d) = %q, want %q", tc.out, tc.rc, got, tc.want)
			}
		})
	}
}

func TestParseExitIP(t *testing.T) {
	if ip, cc, ok := parseExitIP(`{"ip":"1.2.3.4","cc":"NL"}`); !ok || ip != "1.2.3.4" || cc != "NL" {
		t.Errorf("got %q %q %v", ip, cc, ok)
	}
	if ip, _, ok := parseExitIP(`{"ip":"  5.6.7.8  ","cc":""}`); !ok || ip != "5.6.7.8" {
		t.Errorf("trim/empty-cc: got %q %v", ip, ok)
	}
	if _, _, ok := parseExitIP(`{"ip":""}`); ok {
		t.Error("empty ip must be not-ok")
	}
	if _, _, ok := parseExitIP("not json"); ok {
		t.Error("bad json must be not-ok")
	}
}

func TestIPLabel(t *testing.T) {
	if got := ipLabel("1.2.3.4", "NL"); got != "1.2.3.4 (NL)" {
		t.Errorf("ipLabel = %q", got)
	}
	if got := ipLabel("1.2.3.4", ""); got != "1.2.3.4" {
		t.Errorf("ipLabel(no cc) = %q", got)
	}
}

func TestSingboxVersions(t *testing.T) {
	v, ok := parseSingboxVersions(`{"local":"1.9.0","remote":"1.10.0"}`)
	if !ok || v.Local != "1.9.0" || v.Remote != "1.10.0" {
		t.Fatalf("parse = %+v ok=%v", v, ok)
	}
	if _, ok := parseSingboxVersions("not json"); ok {
		t.Error("parse of garbage should fail")
	}
}

func TestConnectionMenuPresentation(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		running, failed                   []string
		ip                                string
		wantHeader, wantDetail, wantColor string
	}{
		{"idle", nil, nil, "", "Disconnected", "Connect to secure your traffic", "stop"},
		{"connected", []string{"Jorbique VPN"}, nil, "1.2.3.4 (NL)", "Connected", "IP  1.2.3.4 (NL)", "ok"},
		{"ip pending", []string{"Jorbique VPN"}, nil, "", "Connected", "Checking public IP…", "ok"},
		{"failed", []string{"Jorbique VPN"}, []string{"Jorbique VPN"}, "1.2.3.4", "Connection needs attention", "DNS", "stop"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ms := MenuStatus{VPNLines: []VPNLine{{Name: "Jorbique VPN", Connected: true, Connectivity: "http FAIL (dns)"}}}
			p := computeStatusPresentation(tc.running, tc.failed)
			detail := connectionDetail(ms, len(tc.running) > 0, tc.ip, tc.failed)
			if p.header != tc.wantHeader || p.color != tc.wantColor || detail != tc.wantDetail {
				t.Errorf("presentation = %+v, detail = %q", p, detail)
			}
		})
	}
}

func TestParseProgressLine(t *testing.T) {
	if pct, ok := parseProgressLine("PROGRESS 42"); !ok || pct != 42 {
		t.Errorf("PROGRESS 42 -> %d %v", pct, ok)
	}
	if pct, ok := parseProgressLine("  PROGRESS 100 "); !ok || pct != 100 {
		t.Errorf("trimmed PROGRESS 100 -> %d %v", pct, ok)
	}
	for _, line := range []string{"", "Downloading sing-box…", "PROGRESS", "PROGRESS abc", "progress 5"} {
		if _, ok := parseProgressLine(line); ok {
			t.Errorf("non-progress line %q parsed as progress", line)
		}
	}
}

func TestValidSingboxUUID(t *testing.T) {
	valid := []string{
		"7998568d-7ae4-4f65-a4cb-b20e78af6a87",
		"7998568D-7AE4-4F65-A4CB-B20E78AF6A87", // uppercase
		"11111111-2222-3333-4444-555555555555",
		"  7998568d-7ae4-4f65-a4cb-b20e78af6a87  ", // trimmed
	}
	for _, s := range valid {
		if !validSingboxUUID(s) {
			t.Errorf("validSingboxUUID(%q) = false, want true", s)
		}
	}
	invalid := []string{
		"",
		"not-a-uuid",
		"7998568d7ae44f65a4cbb20e78af6a87",           // no hyphens
		"7998568d-7ae4-4f65-a4cb-b20e78af6a8",        // last group too short (11)
		"7998568d-7ae4-4f65-a4cb-b20e78af6a877",      // last group too long (13)
		"zzzzzzzz-7ae4-4f65-a4cb-b20e78af6a87",       // non-hex
		"7998568d-7ae4-4f65-a4cb-b20e78af6a87-extra", // extra group
		"7998568d-7ae4-4f65-b20e78af6a87",            // too few groups
	}
	for _, s := range invalid {
		if validSingboxUUID(s) {
			t.Errorf("validSingboxUUID(%q) = true, want false", s)
		}
	}
}

func TestComputeAutostartState(t *testing.T) {
	st := computeAutostartState(MenuStatus{
		AutostartLine:    "VPN autostart: installed & loaded",
		BarAutostartLine: "App autostart: enabled",
		SudoersLine:      "binary in sudoers (NOPASSWD): yes (/opt/old/JorbiqueVPN)",
	})
	if !st.vpnOK || !st.barOK || !st.suOK || !st.vpnInstalled || !st.barInstalled {
		t.Errorf("all-OK state = %+v", st)
	}

	st = computeAutostartState(MenuStatus{
		AutostartLine:    "VPN autostart: installed, not loaded",
		BarAutostartLine: "App autostart: disabled",
		SudoersLine:      "sudoers: —",
	})
	if st.vpnOK || st.barOK || st.suOK {
		t.Errorf("not-OK flags wrong: %+v", st)
	}
	if !st.vpnInstalled { // "installed, not loaded" still counts as installed
		t.Error("vpnInstalled should be true for 'installed, not loaded'")
	}
	if st.barInstalled {
		t.Error("barInstalled should be false for 'disabled'")
	}

	st = computeAutostartState(MenuStatus{SudoersLine: "sudoers: running as root"})
	if !st.suOK {
		t.Error("'running as root' should be OK")
	}
}

// The header used to report whether the LaunchDaemon was installed. It always
// is now — it is the client's privileged component — so the line that flips
// with the menu item is the boot-autostart marker, and the one next to it says
// whether there is a daemon to talk to at all.
func TestTheHeaderReportsTheDaemonAndTheBootChoice(t *testing.T) {
	on := parseMenuStatus("privileged daemon: running (/var/run/jorbique-vpn/control.sock)\nVPN boot autostart: on\nApp autostart: enabled\n")
	if !strings.Contains(on.SudoersLine, "privileged daemon: running") {
		t.Errorf("daemon line = %q", on.SudoersLine)
	}
	st := computeAutostartState(on)
	if !st.suOK {
		t.Error("a running daemon is not shown as healthy")
	}
	if !st.vpnOK || !st.vpnInstalled {
		t.Error("boot autostart on is not shown as on")
	}

	off := computeAutostartState(parseMenuStatus("privileged daemon: not running\nVPN boot autostart: off\n"))
	if off.suOK {
		t.Error("a daemon that is not running is shown as healthy")
	}
	if off.vpnOK || off.vpnInstalled {
		t.Error("boot autostart off still shows the Remove item")
	}

	// An older client on the other end still says what it used to.
	legacy := computeAutostartState(parseMenuStatus("binary in sudoers (NOPASSWD): yes (/x)\nVPN autostart: installed & loaded\n"))
	if !legacy.suOK || !legacy.vpnOK {
		t.Error("an older client's status is no longer understood")
	}
}
