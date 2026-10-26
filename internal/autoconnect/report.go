package autoconnect

import (
	"encoding/json"
	"os"
	"path/filepath"

	"jorbiquevpn/internal/jsonorder"
)

// Protocol states, as the windows draw them.
const (
	StateActive  = "active"  // the tunnel started on it
	StateStandby = "standby" // passed the check; the tunnel fails over to it
	StateFailed  = "failed"  // checked and did not work
	StateSkipped = "skipped" // never checked: Naive won first, or time ran out
)

// ProtocolStatus is one row of the protocol list. Like Progress it carries
// only display information: a tag, an outcome and the measured numbers.
type ProtocolStatus struct {
	Tag       string  `json:"tag"`
	State     string  `json:"state"`
	LatencyMs int64   `json:"latencyMs,omitempty"`
	SpeedKiBs float64 `json:"speedKiBs,omitempty"`
}

// Report is the last connection check: every protocol the profile offered,
// in the server's order, and what the check made of each.
//
// Active is the protocol the tunnel started on. With the local API gone
// nothing reports a later failover, so it stays the check's winner until the
// next connect.
type Report struct {
	Active    string           `json:"active"`
	Naive     bool             `json:"naive"`
	Protocols []ProtocolStatus `json:"protocols"`
}

// NewReport lays the result over the profile's candidates.
func NewReport(profile *jsonorder.Map, r Result) Report {
	working := map[string]Measurement{}
	for _, m := range r.Working {
		working[m.Tag] = m
	}
	failed := map[string]bool{}
	for _, tag := range r.Failed {
		failed[tag] = true
	}
	report := Report{Active: r.Winner(), Naive: r.Naive, Protocols: []ProtocolStatus{}}
	for _, tag := range Candidates(profile) {
		row := ProtocolStatus{Tag: tag, State: StateSkipped}
		if m, ok := working[tag]; ok {
			row.State = StateStandby
			if tag == report.Active {
				row.State = StateActive
			}
			row.LatencyMs = m.Latency.Milliseconds()
			if m.Elapsed > 0 {
				row.SpeedKiBs = float64(m.Bytes) / 1024 / m.Elapsed.Seconds()
			}
		} else if failed[tag] {
			row.State = StateFailed
		}
		report.Protocols = append(report.Protocols, row)
	}
	return report
}

const ReportFile = "connection-report.json"

// WriteReport publishes the report to the macOS menu-bar app the same way
// WriteProgress does: public-readable, display only, replaced atomically.
func WriteReport(runDir string, report Report) {
	data, err := json.Marshal(report)
	if err != nil {
		return
	}
	writePublic(runDir, ReportFile, data)
}

// ReadReport reads what WriteReport left; ok is false when there is none.
func ReadReport(runDir string) (Report, bool) {
	data, err := os.ReadFile(filepath.Join(runDir, ReportFile))
	if err != nil {
		return Report{}, false
	}
	var report Report
	if json.Unmarshal(data, &report) != nil || report.Active == "" {
		return Report{}, false
	}
	return report, true
}

// RemoveReport drops the report of a connection that is being replaced.
func RemoveReport(runDir string) {
	_ = os.Remove(filepath.Join(runDir, ReportFile))
}
