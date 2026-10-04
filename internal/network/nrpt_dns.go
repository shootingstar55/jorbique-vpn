package network

type nrptRule struct {
	namespace   string
	nameServers []string
}

// SetNrptDnsCapture sends every DNS query into the tunnel, with one catch-all
// rule naming the TUN as the resolver.
//
// That is the whole policy. Windows resolves through the per-adapter (ISP) DNS
// and asks every adapter at once; NRPT overrides that globally, the gateway IP
// is in-subnet so the queries enter the TUN, and hijack-dns takes over. Which
// resolver answers a name is then sing-box's own dns rules to decide.
func SetNrptDnsCapture(tunDnsIP string, logFn func(string, string)) {
	err := writeNrptRulesNative([]nrptRule{{namespace: ".", nameServers: []string{tunDnsIP}}})
	if logFn == nil {
		return
	}
	if err != nil {
		logFn("NRPT DNS capture failed (system DNS may bypass the tunnel): "+err.Error(), "WARNING")
		return
	}
	logFn("NRPT DNS capture active (catch-all -> "+tunDnsIP+")", "INFO")
}

// RemoveNrptDnsCapture removes every NRPT rule tagged by this app. Must run on
// disconnect, failed connects, and app start after a crash: a lingering
// catch-all rule with the TUN gone leaves the machine without DNS.
func RemoveNrptDnsCapture(logFn func(string, string)) {
	err := removeNrptRulesNative()
	if logFn != nil && err != nil {
		logFn("NRPT rule cleanup failed: "+err.Error(), "WARNING")
	}
}
