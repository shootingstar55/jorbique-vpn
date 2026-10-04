package netsetup

import (
	"context"
	"strings"
	"sync"
	"time"

	"jorbiquevpn/internal/macos/procutil"
)

var (
	cachedPrimaryServiceMu sync.Mutex
	cachedPrimaryService   string
	cachedPrimaryAt        time.Time

	cachedActiveServiceMu sync.Mutex
	cachedActiveService   string
	cachedActiveAt        time.Time

	cacheTTL = 5 * time.Second
)

// GetPrimaryNetworkService returns the first enabled network service in system order (SystemConfiguration).
func GetPrimaryNetworkService() string {
	cachedPrimaryServiceMu.Lock()
	if cachedPrimaryService != "" && time.Since(cachedPrimaryAt) < cacheTTL {
		s := cachedPrimaryService
		cachedPrimaryServiceMu.Unlock()
		return s
	}
	cachedPrimaryServiceMu.Unlock()

	for _, svc := range ListEnabledNetworkServicesInOrder() {
		cachedPrimaryServiceMu.Lock()
		cachedPrimaryService = svc
		cachedPrimaryAt = time.Now()
		cachedPrimaryServiceMu.Unlock()
		return svc
	}
	return ""
}

// ListEnabledNetworkServicesInOrder returns enabled services in system service order.
func ListEnabledNetworkServicesInOrder() []string {
	return listEnabledNetworkServicesFromSC()
}

// ServiceHasIP reports whether the service's BSD interface has an IPv4 address (from the OS).
func ServiceHasIP(service string) bool {
	return interfaceHasIPv4(bsdNameForNetworkService(service))
}

// NetworkSetup answers what the network is doing: whether it is connected,
// on which interface, and which Wi-Fi.
type NetworkSetup struct {
	wifiService string
}

func New(wifiService string) *NetworkSetup {
	return &NetworkSetup{wifiService: strings.TrimSpace(wifiService)}
}

// findActiveNetworkService picks the preferred service when it has an IP, otherwise the first
// enabled service in system order that has an IP (e.g. USB/Ethernet when Wi-Fi is idle).
func (n *NetworkSetup) findActiveNetworkService() string {
	preferred := strings.TrimSpace(n.wifiService)
	if preferred != "" && ServiceHasIP(preferred) {
		return preferred
	}
	for _, svc := range ListEnabledNetworkServicesInOrder() {
		if ServiceHasIP(svc) {
			return svc
		}
	}
	if preferred != "" {
		return preferred
	}
	if s := GetPrimaryNetworkService(); s != "" {
		return s
	}
	return "Wi-Fi"
}

func (n *NetworkSetup) activeService() string {
	cachedActiveServiceMu.Lock()
	if cachedActiveService != "" && time.Since(cachedActiveAt) < cacheTTL {
		s := cachedActiveService
		cachedActiveServiceMu.Unlock()
		// Return the cached service directly. Re-validating with ServiceHasIP here (net.InterfaceByName
		// + Addrs() on every call) would defeat the 5s cache; the TTL already bounds staleness.
		return s
	}
	cachedActiveServiceMu.Unlock()

	s := n.findActiveNetworkService()
	cachedActiveServiceMu.Lock()
	cachedActiveService = s
	cachedActiveAt = time.Now()
	cachedActiveServiceMu.Unlock()
	return s
}

func (n *NetworkSetup) IsNetworkConnected() bool {
	return ServiceHasIP(n.activeService())
}

// WaitForNetworkConnection polls for an active network up to timeoutSeconds. It honors ctx so a
// caller (e.g. a wake-triggered autostart check) can abort the wait when the machine sleeps again
// instead of leaving a goroutine blocked for the whole timeout.
//
// timeoutSeconds <= 0 does not skip the check: the poll loop runs zero times and the function falls
// through to a single immediate IsNetworkConnected() probe, returning the live result. Callers must
// not pass 0 expecting an unconditional false.
func (n *NetworkSetup) WaitForNetworkConnection(ctx context.Context, timeoutSeconds int) bool {
	return waitForConnection(ctx, timeoutSeconds, n.IsNetworkConnected)
}

// waitForConnection polls connected once per second up to timeoutSeconds, returning early if it
// reports connected or if ctx is cancelled, and doing one final check after the loop so a network
// that comes up on the last tick is still reported. A timeoutSeconds of 0 performs a single
// immediate check. Split out from WaitForNetworkConnection so the poll/cancel logic is unit-testable
// without real network state.
func waitForConnection(ctx context.Context, timeoutSeconds int, connected func() bool) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	for waited := 0; waited < timeoutSeconds; waited++ {
		if connected() {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(time.Second):
		}
	}
	return connected()
}

func (n *NetworkSetup) GetNetworkDevice() string {
	svc := n.activeService()
	if dev := bsdNameForNetworkService(svc); dev != "" {
		return dev
	}
	return "en0"
}

func (n *NetworkSetup) GetCurrentSSID() string {
	device := n.GetNetworkDevice()
	if device == "" {
		return ""
	}
	st, out := procutil.RunAndCapture("/usr/sbin/networksetup", "-getairportnetwork", device)
	if st != 0 {
		return ""
	}
	const prefix = "Current Wi-Fi Network:"
	idx := strings.Index(out, prefix)
	if idx < 0 {
		return ""
	}
	ssid := strings.TrimSpace(out[idx+len(prefix):])
	return ssid
}

func (n *NetworkSetup) GetCurrentIP() string {
	return ipv4OnInterface(n.GetNetworkDevice())
}
