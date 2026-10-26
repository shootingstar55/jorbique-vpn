package core

import (
	"encoding/json"
	"errors"
	"sync"

	"github.com/sagernet/sing-box/experimental/libbox"
)

// Platform is what the Android side provides: the VpnService and the
// system's view of the network. Everything else libbox asks a platform for
// (shells, bridges, Tailscale, notifications) the client does not use, and
// the adapter below answers it here.
type Platform interface {
	// OpenTun establishes the VPN interface described by the JSON (see
	// tunRequest) and returns its file descriptor.
	OpenTun(request string) (int32, error)
	// ProtectSocket keeps a socket the engine opens out of the tunnel.
	ProtectSocket(fd int32) error
	// StartNetworkMonitor begins reporting the default network through
	// UpdateDefaultInterface; StopNetworkMonitor ends it.
	StartNetworkMonitor() error
	StopNetworkMonitor() error
	// NetworkInterfaces lists the physical networks as JSON (see netInterface).
	NetworkInterfaces() (string, error)
	// QueryDNS answers a raw DNS message through the default physical
	// network's resolver.
	QueryDNS(query []byte) ([]byte, error)
}

// tunRequest is OpenTun's argument: what the VpnService.Builder needs.
type tunRequest struct {
	MTU            int32    `json:"mtu"`
	Addresses      []string `json:"addresses"`
	Routes         []string `json:"routes"`
	ExcludeRoutes  []string `json:"excludeRoutes"`
	DNSServers     []string `json:"dnsServers"`
	IncludePackage []string `json:"includePackage"`
	ExcludePackage []string `json:"excludePackage"`
}

// netInterface is one row of NetworkInterfaces.
type netInterface struct {
	Index     int32    `json:"index"`
	MTU       int32    `json:"mtu"`
	Name      string   `json:"name"`
	Addresses []string `json:"addresses"`
	Flags     int32    `json:"flags"`
	Type      int32    `json:"type"`
	DNS       []string `json:"dns"`
	Gateways  []string `json:"gateways"`
	Metered   bool     `json:"metered"`
}

var (
	platformMu sync.Mutex
	current    Platform
)

// SetPlatform hands the running VpnService to the engine. nil when it goes.
func SetPlatform(p Platform) {
	platformMu.Lock()
	current = p
	platformMu.Unlock()
}

func activePlatform() (Platform, error) {
	platformMu.Lock()
	defer platformMu.Unlock()
	if current == nil {
		return nil, errors.New("VPN service is not running")
	}
	return current, nil
}

// The default network is reported once by Android and then on every change,
// while libbox registers a listener per engine instance (the probe engine
// and the tunnel are two). The last report is kept so a listener that comes
// later learns the network at once.
var (
	monitorMu  sync.Mutex
	listeners  = map[libbox.InterfaceUpdateListener]struct{}{}
	lastUpdate *defaultInterface
)

type defaultInterface struct {
	name                   string
	index                  int32
	expensive, constrained bool
}

// UpdateDefaultInterface is how the network monitor reports the default
// physical network; index -1 means there is none.
func UpdateDefaultInterface(name string, index int32, expensive, constrained bool) {
	monitorMu.Lock()
	lastUpdate = &defaultInterface{name, index, expensive, constrained}
	targets := make([]libbox.InterfaceUpdateListener, 0, len(listeners))
	for l := range listeners {
		targets = append(targets, l)
	}
	monitorMu.Unlock()
	for _, l := range targets {
		l.UpdateDefaultInterface(name, index, expensive, constrained)
	}
}

// platformAdapter is libbox's platform interface over Platform.
type platformAdapter struct{}

var _ libbox.PlatformInterface = platformAdapter{}

func (platformAdapter) LocalDNSTransport() libbox.LocalDNSTransport { return dnsTransport{} }

func (platformAdapter) UsePlatformAutoDetectInterfaceControl() bool { return true }

func (platformAdapter) AutoDetectInterfaceControl(fd int32) error {
	p, err := activePlatform()
	if err != nil {
		return err
	}
	return p.ProtectSocket(fd)
}

func (a platformAdapter) BindInterfaceControl(fd int32, _ string) error {
	return a.AutoDetectInterfaceControl(fd)
}

func (platformAdapter) OpenTun(options libbox.TunOptions) (int32, error) {
	p, err := activePlatform()
	if err != nil {
		return -1, err
	}
	request := tunRequest{MTU: options.GetMTU()}
	request.Addresses = append(prefixes(options.GetInet4Address()), prefixes(options.GetInet6Address())...)
	if options.GetAutoRoute() {
		request.Routes = append(prefixes(options.GetInet4RouteRange()), prefixes(options.GetInet6RouteRange())...)
		request.ExcludeRoutes = append(prefixes(options.GetInet4RouteExcludeAddress()), prefixes(options.GetInet6RouteExcludeAddress())...)
	}
	if dns, err := options.GetDNSServerAddress(); err == nil {
		request.DNSServers = stringList(dns)
	}
	request.IncludePackage = stringList(options.GetIncludePackage())
	request.ExcludePackage = stringList(options.GetExcludePackage())
	body, err := json.Marshal(request)
	if err != nil {
		return -1, err
	}
	return p.OpenTun(string(body))
}

func prefixes(it libbox.RoutePrefixIterator) []string {
	var out []string
	for it != nil && it.HasNext() {
		out = append(out, it.Next().String())
	}
	return out
}

func stringList(it libbox.StringIterator) []string {
	var out []string
	for it != nil && it.HasNext() {
		out = append(out, it.Next())
	}
	return out
}

func (platformAdapter) UseProcFS() bool { return false }

func (platformAdapter) FindConnectionOwner(int32, string, int32, string, int32) (*libbox.ConnectionOwner, error) {
	return nil, errors.ErrUnsupported
}

func (platformAdapter) StartDefaultInterfaceMonitor(listener libbox.InterfaceUpdateListener) error {
	p, err := activePlatform()
	if err != nil {
		return err
	}
	monitorMu.Lock()
	first := len(listeners) == 0
	listeners[listener] = struct{}{}
	last := lastUpdate
	monitorMu.Unlock()
	if first {
		if err := p.StartNetworkMonitor(); err != nil {
			return err
		}
	} else if last != nil {
		listener.UpdateDefaultInterface(last.name, last.index, last.expensive, last.constrained)
	}
	return nil
}

func (platformAdapter) CloseDefaultInterfaceMonitor(listener libbox.InterfaceUpdateListener) error {
	monitorMu.Lock()
	delete(listeners, listener)
	last := len(listeners) == 0
	if last {
		lastUpdate = nil
	}
	monitorMu.Unlock()
	if !last {
		return nil
	}
	p, err := activePlatform()
	if err != nil {
		return nil
	}
	return p.StopNetworkMonitor()
}

func (platformAdapter) GetInterfaces() (libbox.NetworkInterfaceIterator, error) {
	p, err := activePlatform()
	if err != nil {
		return nil, err
	}
	body, err := p.NetworkInterfaces()
	if err != nil {
		return nil, err
	}
	var rows []netInterface
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		return nil, err
	}
	out := make([]*libbox.NetworkInterface, 0, len(rows))
	for _, row := range rows {
		out = append(out, &libbox.NetworkInterface{
			Index: row.Index, MTU: row.MTU, Name: row.Name, Flags: row.Flags, Type: row.Type,
			Addresses: &stringIterator{row.Addresses}, DNSServer: &stringIterator{row.DNS},
			Gateway: &stringIterator{row.Gateways}, Metered: row.Metered,
		})
	}
	return &interfaceIterator{out}, nil
}

func (platformAdapter) UnderNetworkExtension() bool                 { return false }
func (platformAdapter) IncludeAllNetworks() bool                    { return false }
func (platformAdapter) ReadWIFIState() *libbox.WIFIState            { return nil }
func (platformAdapter) ClearDNSCache()                              {}
func (platformAdapter) SendNotification(*libbox.Notification) error { return nil }
func (platformAdapter) CancelNotification(string, int32) error      { return nil }
func (platformAdapter) StartNeighborMonitor(libbox.NeighborUpdateListener) error {
	return errors.ErrUnsupported
}
func (platformAdapter) CloseNeighborMonitor(libbox.NeighborUpdateListener) error { return nil }
func (platformAdapter) RegisterMyInterface(string)                               {}
func (platformAdapter) UsePlatformShell() bool                                   { return false }
func (platformAdapter) CheckPlatformShell() error                                { return errors.ErrUnsupported }
func (platformAdapter) OpenShellSession(*libbox.PlatformUser, string, libbox.StringIterator, string, int32, int32) (libbox.ShellSession, error) {
	return nil, errors.ErrUnsupported
}
func (platformAdapter) LookupUser(string) (*libbox.PlatformUser, error) {
	return nil, errors.ErrUnsupported
}
func (platformAdapter) LookupSFTPServer() (string, error)     { return "", errors.ErrUnsupported }
func (platformAdapter) ReadSystemSSHHostKey() (string, error) { return "", errors.ErrUnsupported }
func (platformAdapter) TailscaleHostname() string             { return "" }
func (platformAdapter) UsePlatformBridge() bool               { return false }
func (platformAdapter) CreateBridge(*libbox.BridgeOptions) (libbox.BridgeSession, error) {
	return nil, errors.ErrUnsupported
}

// dnsTransport answers the profile's `local` DNS server through Android's
// resolver: there is no /etc/resolv.conf for the engine to read.
type dnsTransport struct{}

func (dnsTransport) Raw() bool { return true }

func (dnsTransport) Lookup(*libbox.ExchangeContext, string, string) error {
	return errors.ErrUnsupported
}

func (dnsTransport) Exchange(ctx *libbox.ExchangeContext, message []byte) error {
	p, err := activePlatform()
	if err != nil {
		return err
	}
	answer, err := p.QueryDNS(message)
	if err != nil {
		return err
	}
	ctx.RawSuccess(answer)
	return nil
}

type stringIterator struct{ values []string }

func (i *stringIterator) Len() int32    { return int32(len(i.values)) }
func (i *stringIterator) HasNext() bool { return len(i.values) > 0 }
func (i *stringIterator) Next() string {
	if len(i.values) == 0 {
		return ""
	}
	v := i.values[0]
	i.values = i.values[1:]
	return v
}

type interfaceIterator struct{ values []*libbox.NetworkInterface }

func (i *interfaceIterator) HasNext() bool { return len(i.values) > 0 }
func (i *interfaceIterator) Next() *libbox.NetworkInterface {
	if len(i.values) == 0 {
		return nil
	}
	v := i.values[0]
	i.values = i.values[1:]
	return v
}

// commandHandler answers the engine's requests to the app. The client drives
// the engine itself, so there is nothing for it to do.
type commandHandler struct{}

func (commandHandler) ServiceStop() error   { return nil }
func (commandHandler) ServiceReload() error { return nil }
func (commandHandler) GetSystemProxyStatus() (*libbox.SystemProxyStatus, error) {
	return &libbox.SystemProxyStatus{}, nil
}
func (commandHandler) SetSystemProxyEnabled(bool) error { return nil }
func (commandHandler) TriggerNativeCrash() error        { return nil }
func (commandHandler) WriteDebugMessage(string)         {}
func (commandHandler) ConnectSSHAgent() (int32, error)  { return -1, errors.ErrUnsupported }
