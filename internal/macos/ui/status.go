package ui

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"jorbiquevpn/internal/macos/ansiutil"
)

// parseProgressLine extracts the percent from a "PROGRESS <n>" line emitted by the
// update-singbox subprocess. ok is false for any other line, so non-progress
// output (normal logs) is ignored by the download progress window.
func parseProgressLine(line string) (int, bool) {
	const prefix = "PROGRESS "
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, prefix) {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, prefix)))
	if err != nil {
		return 0, false
	}
	return n, true
}

// parseExitIP parses the `exit-ip` JSON ({"ip","cc"}). ok is false when there is
// no usable IP. Ports the JSON handling in uiFetchPublicIPAndCountry.
func parseExitIP(jsonOut string) (ip, cc string, ok bool) {
	var m struct {
		IP string `json:"ip"`
		CC string `json:"cc"`
	}
	if json.Unmarshal([]byte(jsonOut), &m) != nil {
		return "", "", false
	}
	ip = strings.TrimSpace(m.IP)
	cc = strings.TrimSpace(m.CC)
	if ip == "" {
		return "", "", false
	}
	return ip, cc, true
}

// ipLabel formats an exit IP as "ip (cc)" or just "ip" when the country is empty.
func ipLabel(ip, cc string) string {
	if cc != "" {
		return ip + " (" + cc + ")"
	}
	return ip
}

// singboxVersionInfo holds the `singbox-versions` JSON: the installed (local)
// version and the version advertised by the single update server.
type singboxVersionInfo struct {
	Local  string `json:"local"`
	Remote string `json:"remote"`
}

// parseSingboxVersions parses the `singbox-versions` JSON (local + per-channel
// advertised versions), used by the silent launch-time auto-update.
func parseSingboxVersions(jsonOut string) (singboxVersionInfo, bool) {
	var v singboxVersionInfo
	if json.Unmarshal([]byte(jsonOut), &v) != nil {
		return singboxVersionInfo{}, false
	}
	return v, true
}

// uuidPlaceholder is the canonical UUID shape shown in the sing-box field and the
// validation error, e.g. 7998568d-7ae4-4f65-a4cb-b20e78af6a87.
const uuidPlaceholder = "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"

// connectedSuffix ends a status line that reports a running VPN.
const connectedSuffix = " connected"

// validSingboxUUID reports whether s is a canonical UUID: five hyphen-separated
// groups of 8-4-4-4-12 hexadecimal digits (case-insensitive). This guards the
// sing-box UUID field so only a well-formed id is saved.
func validSingboxUUID(s string) bool {
	groups := [...]int{8, 4, 4, 4, 12}
	parts := strings.Split(strings.TrimSpace(s), "-")
	if len(parts) != len(groups) {
		return false
	}
	for i, part := range parts {
		if len(part) != groups[i] {
			return false
		}
		for _, r := range part {
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				return false
			}
		}
	}
	return true
}

// autostartState is the parsed health of the autostart/sudoers footer lines,
// driving both their color and which install/uninstall submenu items show.
type autostartState struct {
	vpnOK, barOK, suOK         bool // green when true, red otherwise
	vpnInstalled, barInstalled bool // toggles Install↔Remove / Enable↔Disable
}

// computeAutostartState derives the footer health from the parsed status lines.
// Ports the string checks in updateAutostartMenuItems.
func computeAutostartState(ms MenuStatus) autostartState {
	// "on" is the boot-autostart marker; "installed & loaded" is what an older
	// client printed, kept so a menu bar talking to one still reads it.
	vpnOK := strings.Contains(ms.AutostartLine, ": on") || strings.Contains(ms.AutostartLine, "installed & loaded")
	barOK := strings.Contains(ms.BarAutostartLine, "enabled")
	// ": running" and not "running", because "not running" contains the word.
	suOK := strings.Contains(ms.SudoersLine, ": running") ||
		strings.Contains(ms.SudoersLine, "running as root") || strings.Contains(ms.SudoersLine, "yes (")
	return autostartState{
		vpnOK:        vpnOK,
		barOK:        barOK,
		suOK:         suOK,
		vpnInstalled: vpnOK || strings.Contains(ms.AutostartLine, "installed, not loaded"),
		barInstalled: barOK || strings.Contains(ms.BarAutostartLine, "installed, not loaded"),
	}
}

// This file ports the pure text-parsing helpers from ui.m (uiStatusBarTitle,
// uiTrimLeadingStatusGlyph, uiConnectivityHasHttpFail, and the status-output
// parsing inside uiGetStatusForMenu / uiGetRunningVPNs). The Objective-C code
// interleaved running the `status` subprocess with parsing its stdout; here the
// two are separated so the parsing is exercised by table-driven tests with no
// subprocess. The exec layer (exec.go) runs the CLI and feeds the raw text in.

// circledNumbers are the status-bar glyphs for 1..20 running VPNs.
var circledNumbers = []string{
	"①", "②", "③", "④", "⑤", "⑥", "⑦", "⑧", "⑨", "⑩",
	"⑪", "⑫", "⑬", "⑭", "⑮", "⑯", "⑰", "⑱", "⑲", "⑳",
}

// statusBarTitle returns the menu-bar glyph for the given number of running
// VPNs: a circled digit for 1..20, "20+" beyond that, and an empty circle for
// none. Ports uiStatusBarTitle.
func statusBarTitle(runningCount int) string {
	if runningCount >= 1 && runningCount <= 20 {
		return circledNumbers[runningCount-1]
	}
	if runningCount > 20 {
		return "20+"
	}
	return "○"
}

// trimLeadingStatusGlyph strips a leading ✓ or ✗ (and surrounding whitespace)
// from a `status` line before the VPN name is extracted. A non-glyph line is
// returned whitespace-trimmed and otherwise unchanged. Ports
// uiTrimLeadingStatusGlyph; the Objective-C version used
// rangeOfComposedCharacterSequenceAtIndex to handle multi-scalar emoji, which in
// Go is naturally expressed by decoding the first rune (both glyphs here are
// single runes, but trimming a prefix keeps the same contract).
func trimLeadingStatusGlyph(before string) string {
	t := strings.TrimSpace(before)
	if t == "" {
		return ""
	}
	for _, g := range []string{"✓", "✗"} {
		if strings.HasPrefix(t, g) {
			return strings.TrimSpace(t[len(g):])
		}
	}
	return t
}

// alertBody turns a failed command's captured output and exit code into an
// alert body: the ANSI-stripped, trimmed output, or "Exit code N" when empty.
// Ports the body construction shared by the cmd* error alerts in ui.m.
func alertBody(out string, exitCode int) string {
	body := strings.TrimSpace(ansiutil.Strip(out))
	if body == "" {
		return fmt.Sprintf("Exit code %d", exitCode)
	}
	return body
}

// connectivityHasHTTPFail reports whether a connectivity string indicates the
// HTTP reachability check failed. Ports uiConnectivityHasHttpFail: case
// insensitive, true when it mentions both HTTP and FAIL but is not "HTTP OK".
func connectivityHasHTTPFail(s string) bool {
	if s == "" {
		return false
	}
	u := strings.ToUpper(s)
	return strings.Contains(u, "HTTP") && strings.Contains(u, "FAIL") && !strings.Contains(u, "HTTP OK")
}

// connectivityReason extracts the parenthesized verdict the CLI appends to a
// failed check ("http FAIL (dns)" -> "dns"), or "" when the line carries none.
// The reason is the last parenthesized group, so a host name with brackets in
// it cannot be mistaken for one.
func connectivityReason(conn string) string {
	open := strings.LastIndex(conn, "(")
	if open < 0 {
		return ""
	}
	rest := conn[open+1:]
	end := strings.Index(rest, ")")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}

// connFailLabel turns a probe's reason into the few words the menu bar has room
// for. "http FAIL" used to be the whole story the user got, which is wrong twice
// over when the probe never resolved the name: it names the wrong subsystem, and
// it sends the reader to the web service instead of to DNS.
func connFailLabel(reason string) string {
	switch {
	case reason == "":
		return "unreachable"
	case reason == "dns":
		return "DNS"
	case strings.HasPrefix(reason, "http "):
		return "HTTP " + strings.TrimPrefix(reason, "http ")
	case reason == "http error":
		return "HTTP error"
	default:
		return reason
	}
}

// VPNLine is one VPN's state parsed from `status` output. Connectivity is empty
// when the line carried no connectivity detail (the Objective-C NSNull case).
type VPNLine struct {
	Name         string
	Connected    bool
	Connectivity string
}

// MenuStatus is the parsed `status` output the menu header renders from. Ports
// the dictionary returned by uiGetStatusForMenu. The exit-IP line is fetched
// separately (it is cached from `exit-ip`), so it has no field here.
type MenuStatus struct {
	VPNLines         []VPNLine
	SudoersLine      string
	AutostartLine    string
	BarAutostartLine string

	// Heal gates, emitted by `status` only when some VPN's check failed. Nil
	// means the CLI said nothing (an older binary, or nothing failing), which
	// the healer reads as "no reason to hold back".
	InternetOK    *bool
	UserSessionOK *bool
}

// status header placeholders, shown until a real line replaces them.
const (
	defaultSudoersLine      = "privileged daemon: —"
	defaultAutostartLine    = "VPN boot autostart: —"
	defaultBarAutostartLine = "App autostart: —"
)

// parseMenuStatus parses the stdout of `<exe> status` into the structure the
// menu header renders. It mirrors uiGetStatusForMenu's line classification:
// sudoers / autostart / app-autostart header lines, "<name> connected",
// "<name> stopped", and "connectivity: …" attached to the most recent connected
// VPN of the same name.
func parseMenuStatus(statusOutput string) MenuStatus {
	out := MenuStatus{
		SudoersLine:      defaultSudoersLine,
		AutostartLine:    defaultAutostartLine,
		BarAutostartLine: defaultBarAutostartLine,
	}
	var lastVPN string
	for _, line := range strings.Split(statusOutput, "\n") {
		plain := strings.TrimSpace(ansiutil.Strip(line))
		if plain == "" {
			continue
		}
		switch {
		case strings.HasPrefix(plain, "privileged daemon:"),
			strings.Contains(plain, "binary in sudoers"), strings.HasPrefix(plain, "sudoers:"):
			out.SudoersLine = plain
		case strings.HasPrefix(plain, "VPN boot autostart:"),
			strings.HasPrefix(plain, "VPN autostart:"), strings.HasPrefix(plain, "autostart:"):
			out.AutostartLine = plain
		case strings.HasPrefix(plain, "App autostart:"), strings.HasPrefix(plain, "Bar autostart:"):
			out.BarAutostartLine = plain
		case strings.HasPrefix(plain, "internet:"):
			ok := connectivityValue(plain) == "ok"
			out.InternetOK = &ok
		case strings.HasPrefix(plain, "user session:"):
			ok := connectivityValue(plain) == "active"
			out.UserSessionOK = &ok
		default:
			if name, ok := nameBefore(plain, connectedSuffix); ok {
				out.VPNLines = append(out.VPNLines, VPNLine{Name: name, Connected: true})
				lastVPN = name
			} else if name, ok := nameBefore(plain, " stopped"); ok {
				out.VPNLines = append(out.VPNLines, VPNLine{Name: name, Connected: false})
			} else if strings.Contains(plain, "connectivity") && lastVPN != "" {
				attachConnectivity(out.VPNLines, lastVPN, connectivityValue(plain))
			}
		}
	}
	return out
}

// parseRunningVPNs extracts the names of connected VPNs from `status` output.
// Ports uiGetRunningVPNs. Note it strips ANSI but does not whitespace-trim the
// whole line before searching for " connected" (matching the original, which
// only trims the extracted name).
func parseRunningVPNs(statusOutput string) []string {
	var running []string
	for _, line := range strings.Split(statusOutput, "\n") {
		plain := ansiutil.Strip(line)
		if name, ok := nameBefore(plain, connectedSuffix); ok {
			running = append(running, name)
		}
	}
	return running
}

// connectedNameFromLine reads the VPN out of the "✓ <name> connected" line the
// CLI prints the moment a tunnel finishes coming up. It is the same shape the
// status output uses, which is why it shares nameBefore — but it arrives while
// the start is still running, which is what makes it worth reading: it is the
// only word the menu bar gets about one tunnel before the whole batch is over.
func connectedNameFromLine(line string) (string, bool) {
	return nameBefore(ansiutil.Strip(line), connectedSuffix)
}

// nameBefore finds marker in line and, if present, returns the VPN name in the
// text before it (glyph-trimmed). ok is false when the marker is absent or the
// name is empty.
func nameBefore(line, marker string) (string, bool) {
	idx := strings.Index(line, marker)
	if idx < 0 {
		return "", false
	}
	name := trimLeadingStatusGlyph(strings.TrimSpace(line[:idx]))
	if name == "" {
		return "", false
	}
	return name, true
}

// connectivityValue extracts the text after the first colon of a connectivity
// line, or the whole line if it has no colon.
func connectivityValue(plain string) string {
	if col := strings.Index(plain, ":"); col >= 0 {
		return strings.TrimSpace(plain[col+1:])
	}
	return plain
}

// attachConnectivity sets conn on the most recent connected VPNLine named name,
// scanning from the end (matching the Objective-C reverse search).
func attachConnectivity(lines []VPNLine, name, conn string) {
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i].Name != name || !lines[i].Connected {
			continue
		}
		lines[i].Connectivity = conn
		return
	}
}

// healBlockedReason returns why an automatic restart must not fire right now,
// or "" when nothing blocks it. A failing check alone does not mean the tunnel
// is at fault: with the network down every tunnel fails at once and restarting
// them fixes nothing, and behind the lock screen a restart churns resolvers
// (and re-prompts SSO) for a tunnel nobody is using.
func healBlockedReason(ms MenuStatus) string {
	if ms.InternetOK != nil && !*ms.InternetOK {
		return "no working internet"
	}
	if ms.UserSessionOK != nil && !*ms.UserSessionOK {
		return "no active user session"
	}
	return ""
}

// connectivityFailures returns the names of connected VPNs whose HTTP check
// failed, in order. Used to flag a degraded connection in the status header.
func connectivityFailures(ms MenuStatus) []string {
	var names []string
	for _, v := range ms.VPNLines {
		if v.Connected && connectivityHasHTTPFail(v.Connectivity) {
			names = append(names, v.Name)
		}
	}
	return names
}

// A single failed probe is not a broken VPN. The check is one curl with a
// timeout, run every refresh, and it fails for reasons that have nothing to do
// with the tunnel: a resolver cache still cold right after connect, a service
// restarting behind the ingress, a laptop mid-Wi-Fi-roam. Painting the menu bar
// red on the first one told users their VPN was broken while their browser was
// happily loading pages through it.
//
// So the alarm needs corroboration — two checks in a row, spanning at least
// showFailMinSpan — while a recovery clears it at once. Slow to accuse, quick to
// forgive. The span matters because the bar polls every 2s for half a minute
// after any action: without it, "twice in a row" could mean four seconds, right
// in the window where a tunnel is still settling.
const (
	showFailAfterChecks = 2
	showFailMinSpan     = 10 * time.Second
)

// connFailStreak is one VPN's run of failing checks: how many, and when it began.
type connFailStreak struct {
	count int
	since time.Time
}

// connFailTracker decides which failing checks the menu bar should show. Pure
// bookkeeping, so it is unit-tested without AppKit; the caller owns the instance
// and only touches it on the main thread.
type connFailTracker struct {
	fails map[string]connFailStreak
}

// observe records one refresh's failing VPNs and returns those worth showing.
func (t *connFailTracker) observe(failed []string, now time.Time) []string {
	if t.fails == nil {
		t.fails = map[string]connFailStreak{}
	}
	failing := make(map[string]struct{}, len(failed))
	var confirmed []string
	for _, name := range failed {
		failing[name] = struct{}{}
		streak, ongoing := t.fails[name]
		if !ongoing {
			streak.since = now
		}
		streak.count++
		t.fails[name] = streak
		if streak.count >= showFailAfterChecks && now.Sub(streak.since) >= showFailMinSpan {
			confirmed = append(confirmed, name)
		}
	}
	// A VPN that recovered (or stopped) starts over, so the next failure has to
	// earn the alarm again.
	for name := range t.fails {
		if _, still := failing[name]; !still {
			delete(t.fails, name)
		}
	}
	return confirmed
}

// statusPresentation is the pure decision behind the menu-bar glyph and header:
// given the running VPNs, the parsed status, whether a start happened recently,
// and the app name, it returns the glyph, a color keyword ("ok"/"stop"/
// "yellow"), the header text, and whether the header should be colored (vs the
// muted idle style). Ports the presentation half of refreshStatus, minus the
// exit-IP enrichment which is fetched separately. Kept free of AppKit so it is
// unit-testable on any platform; the caller maps the keyword to an NSColor.
type statusPresentation struct {
	glyph   string
	color   string
	header  string
	colored bool
}

// This is the steady state only. Anything in motion — connecting, disconnecting,
// restarting, quitting — is drawn by the spinner instead (actionprogress.go),
// which owns the glyph for as long as the action runs. There used to be two ⏳
// cases here for "recently started" and "recently stopped"; they were guesses
// from before the spinner existed, showing an hourglass on a timer whether or
// not anything was still happening, in a glyph style used nowhere else.
//
// Three colors carry the state: green connected, yellow working towards
// connected, red off or going off.
// failed is the confirmed set from connFailTracker, not the raw one from this
// refresh: the decision to accuse belongs to the tracker, the wording here.
func computeStatusPresentation(running []string, failed []string) statusPresentation {
	switch {
	case len(running) > 0 && len(failed) > 0:
		return statusPresentation{"⚠", "stop", "Connection needs attention", true}
	case len(running) > 0:
		return statusPresentation{statusBarTitle(len(running)), "ok", "Connected", true}
	default:
		return statusPresentation{"○", "stop", "Disconnected", false}
	}
}

func connectionDetail(ms MenuStatus, connected bool, exitIP string, failed []string) string {
	if !connected {
		return "Connect to secure your traffic"
	}
	if len(failed) > 0 {
		return connFailLabel(connectivityReasonFor(ms, failed[0]))
	}
	if exitIP == "" {
		return "Checking public IP…"
	}
	return "IP  " + exitIP
}

// failureHeader says what is wrong, not merely that something is. One failing
// VPN — the common case — gets its reason spelled out ("⚠ projects: DNS"), which
// is the difference between a user reporting "the VPN is broken" and reporting
// something a maintainer can act on. Several at once get the list: they nearly
// always share one cause (the network is gone), and the reasons would not fit.
func failureHeader(ms MenuStatus, failed []string) string {
	if len(failed) == 1 {
		return "⚠ " + failed[0] + ": " + connFailLabel(connectivityReasonFor(ms, failed[0]))
	}
	return "⚠ Error: " + strings.Join(failed, ", ")
}

// connectivityReasonFor returns the probe reason recorded for a connected VPN.
func connectivityReasonFor(ms MenuStatus, name string) string {
	for _, v := range ms.VPNLines {
		if v.Name == name && v.Connected {
			return connectivityReason(v.Connectivity)
		}
	}
	return ""
}

// compareVersions compares dotted-numeric versions (e.g. "1.13.11" vs "1.13.7"),
// returning -1 if a<b, 0 if equal, 1 if a>b. Components are compared numerically
// (so 11 > 7), a leading "v" is ignored, and any non-numeric suffix on a component
// (e.g. a pre-release tag) ends parsing of that string. Missing components count
// as 0, so "1.13" == "1.13.0".
func compareVersions(a, b string) int {
	pa, pb := versionParts(a), versionParts(b)
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
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
	}
	return 0
}

// versionParts splits a dotted-numeric version into its integer components,
// stopping at the first component that does not start with a digit.
func versionParts(s string) []int {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if s == "" {
		return nil
	}
	out := make([]int, 0, 4)
	for _, field := range strings.Split(s, ".") {
		num, got := 0, false
		for _, r := range field {
			if r < '0' || r > '9' {
				break
			}
			num = num*10 + int(r-'0')
			got = true
		}
		if !got {
			break
		}
		out = append(out, num)
	}
	return out
}
