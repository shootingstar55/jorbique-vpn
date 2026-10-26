package core

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/sagernet/sing-box/experimental/libbox"

	"jorbiquevpn/internal/autoconnect"
	"jorbiquevpn/internal/config"
	"jorbiquevpn/internal/jsonorder"
	"jorbiquevpn/internal/logging"
	"jorbiquevpn/internal/security"
)

// The engine: a connection check over every protocol the server offers, then
// the tunnel on what passed. It is the desktop client's Connect
// (internal/app) with sing-box in process instead of an executable.
var (
	// lifecycleMu serialises Connect and Disconnect, as cleanupMu does on
	// Windows: a connection check holds it for up to the scan limit.
	lifecycleMu sync.Mutex

	stateMu    sync.Mutex
	progress   autoconnect.Progress
	cancelScan context.CancelFunc
	report     *autoconnect.Report
	tunnel     *libbox.CommandServer
)

// Errors the window shows as they are. The UI is Russian; so are these.
var (
	errNoUUID      = errors.New("Сначала введите UUID")
	errCancelled   = errors.New("Подключение отменено")
	errUnavailable = errors.New("Не нашлось ни одного рабочего подключения. Проверьте интернет и UUID и попробуйте ещё раз")
)

// Connect checks the server's protocols and starts the tunnel on the best one.
// It blocks until the tunnel is up or the attempt failed, and runs inside the
// VpnService, which must have called SetPlatform first.
func Connect() error {
	if err := ready(); err != nil {
		return err
	}
	uuid := strings.TrimSpace(store.GetUserUUID())
	if !security.ValidUUID(uuid, false) {
		return errNoUUID
	}
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	stopTunnel()
	setProgress(autoconnect.Progress{Phase: "preparing"})
	defer setProgress(autoconnect.Progress{})
	setReport(nil)
	ipInfoCache.Invalidate()

	remoteJSON, err := config.FetchSingboxClientJSONOrCache(func(msg string) { logf(msg, "WARN") })
	if err != nil {
		return fmt.Errorf("Не удалось загрузить профиль с сервера: %w", err)
	}
	profile := jsonorder.New()
	if err := jsonorder.Unmarshal(remoteJSON, profile); err != nil {
		return errors.New("Не удалось прочитать профиль VPN")
	}

	logf("Finding the best connection (Naive first)", "INFO")
	ctx, cancel := context.WithTimeout(context.Background(), autoconnect.ScanTimeout)
	defer cancel()
	setCancelScan(cancel)
	checker := &prober{profile: remoteJSON, uuid: uuid}
	selection, err := autoconnect.Select(ctx, profile, checker.probe, func(message string) { logf(message, "INFO") }, setProgress)
	checker.close()
	setCancelScan(nil)
	if errors.Is(err, context.Canceled) {
		logf("Connection check cancelled", "INFO")
		return errCancelled
	}
	if err != nil {
		return errUnavailable
	}
	logf("Automatically selected connection: "+selection.Winner(), "INFO")

	setProgress(autoconnect.Progress{Phase: "starting"})
	runtime, err := buildRuntimeConfig(remoteJSON, uuid, selection)
	if err != nil {
		return fmt.Errorf("Не удалось собрать конфигурацию: %w", err)
	}
	server, err := libbox.NewCommandServer(commandHandler{}, platformAdapter{})
	if err != nil {
		return err
	}
	if err := server.StartOrReloadService(runtime, &libbox.OverrideOptions{}); err != nil {
		server.Close()
		logf("sing-box did not start: "+err.Error(), "ERROR")
		return fmt.Errorf("Не удалось запустить sing-box: %w", err)
	}
	stateMu.Lock()
	tunnel = server
	stateMu.Unlock()
	r := autoconnect.NewReport(profile, selection)
	setReport(&r)
	logf("Connected", "INFO")
	return nil
}

// Disconnect stops a running check or tunnel. Safe to call when neither runs.
func Disconnect() {
	CancelConnect()
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	stopTunnel()
	setReport(nil)
	ipInfoCache.Invalidate()
}

func stopTunnel() {
	stateMu.Lock()
	server := tunnel
	tunnel = nil
	stateMu.Unlock()
	if server == nil {
		return
	}
	_ = server.CloseService()
	server.Close()
	logf("Disconnected", "INFO")
}

// IsConnected reports whether the tunnel is up.
func IsConnected() bool {
	stateMu.Lock()
	defer stateMu.Unlock()
	return tunnel != nil
}

// CancelConnect stops a connection check in progress; Connect then fails with
// errCancelled without having touched the network.
func CancelConnect() {
	stateMu.Lock()
	defer stateMu.Unlock()
	if cancelScan != nil {
		cancelScan()
	}
}

// Pause and Wake follow the screen: the engine closes idle connections while
// the phone sleeps, as the official Android client does.
func Pause() {
	stateMu.Lock()
	server := tunnel
	stateMu.Unlock()
	if server != nil {
		server.Pause()
	}
}

func Wake() {
	stateMu.Lock()
	server := tunnel
	stateMu.Unlock()
	if server != nil {
		server.Wake()
	}
}

// ResetNetwork drops connections bound to a network that went away.
func ResetNetwork() {
	stateMu.Lock()
	server := tunnel
	stateMu.Unlock()
	if server != nil {
		server.ResetNetwork()
	}
}

func setProgress(p autoconnect.Progress) {
	stateMu.Lock()
	progress = p
	stateMu.Unlock()
}

func setCancelScan(cancel context.CancelFunc) {
	stateMu.Lock()
	cancelScan = cancel
	stateMu.Unlock()
}

func setReport(r *autoconnect.Report) {
	stateMu.Lock()
	report = r
	stateMu.Unlock()
}

func connectionProgress() autoconnect.Progress {
	stateMu.Lock()
	defer stateMu.Unlock()
	return progress
}

func connectionReport() autoconnect.Report {
	stateMu.Lock()
	defer stateMu.Unlock()
	if report == nil || tunnel == nil {
		return autoconnect.Report{Protocols: []autoconnect.ProtocolStatus{}}
	}
	return *report
}

func logf(message, level string) { logging.Write(message, level) }

// prober runs each check in its own engine instance: a SOCKS inbound on
// loopback, the one protocol under test as the final route, and nothing else
// (autoconnect.ProbeConfig). One command server carries them all, one at a
// time, and goes away with the scan.
type prober struct {
	profile []byte
	uuid    string
	server  *libbox.CommandServer
}

func (p *prober) probe(ctx context.Context, tag string) (autoconnect.Measurement, error) {
	ctx, cancel := context.WithTimeout(ctx, autoconnect.ProbeTimeout)
	defer cancel()
	port, err := freePort()
	if err != nil {
		return autoconnect.Measurement{}, err
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return autoconnect.Measurement{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(random[:])
	root, err := autoconnect.ProbeConfig(p.profile, p.uuid, tag, port, token)
	if err != nil {
		return autoconnect.Measurement{}, err
	}
	body, err := jsonorder.MarshalIndent(root)
	if err != nil {
		return autoconnect.Measurement{}, err
	}
	if p.server == nil {
		if p.server, err = libbox.NewCommandServer(commandHandler{}, platformAdapter{}); err != nil {
			return autoconnect.Measurement{}, err
		}
	}
	// Starting can take a while (Naive loads Chromium's network stack), so it
	// runs beside the deadline rather than in front of it.
	started := make(chan error, 1)
	server := p.server
	go func() { started <- server.StartOrReloadService(string(body), &libbox.OverrideOptions{}) }()
	defer func() { _ = server.CloseService() }()
	select {
	case err := <-started:
		if err != nil {
			// Engine errors can carry server details; the log gets the tag only.
			return autoconnect.Measurement{}, fmt.Errorf("connection check did not start")
		}
	case <-ctx.Done():
		return autoconnect.Measurement{}, ctx.Err()
	}
	address := net.JoinHostPort("127.0.0.1", fmt.Sprint(port))
	return autoconnect.MeasureProxy(ctx, address, token)
}

func (p *prober) close() {
	if p.server != nil {
		_ = p.server.CloseService()
		p.server.Close()
		p.server = nil
	}
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// buildRuntimeConfig is the desktop build of the tunnel config — the same
// protocol selection, failover group, account and direct domains — with the
// parts that belong to Windows swapped for Android ones: the tun inbound the
// VpnService opens, apps excluded by package instead of by process, and
// absolute paths for the log and the cache.
func buildRuntimeConfig(remoteJSON []byte, uuid string, selection autoconnect.Result) (string, error) {
	profile := jsonorder.New()
	if err := jsonorder.Unmarshal(remoteJSON, profile); err != nil {
		return "", err
	}
	path, err := config.BuildRuntimeConfig(remoteJSON, config.RuntimeOptions{
		UserUUID: uuid, OutboundEnabled: selection.Enabled(profile), NaiveMode: selection.Naive,
		UserDirectDomains: store.GetDirectDomains(), Automatic: &selection,
	})
	if err != nil {
		return "", err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	cfg := jsonorder.New()
	if err := jsonorder.Unmarshal(body, cfg); err != nil {
		return "", err
	}
	adaptForAndroid(cfg, store.GetIgnoredProcessNames())
	out, err := jsonorder.MarshalIndent(cfg)
	if err != nil {
		return "", err
	}
	_ = os.WriteFile(path, out, 0600)
	return string(out), nil
}

// tunMTU is what the official Android client uses: the VpnService interface
// is a pipe, not a link, so a large MTU only saves work.
const tunMTU = 9000

func adaptForAndroid(cfg *jsonorder.Map, excludedPackages []string) {
	tun := jsonorder.New()
	tun.Set("type", "tun")
	tun.Set("tag", "tun-in")
	// An IPv6 address too, so IPv6 enters the tunnel and meets the profile's
	// reject rule instead of leaving beside it.
	tun.Set("address", []any{"172.19.0.1/30", "fdfe:dcba:9876::1/126"})
	tun.Set("mtu", tunMTU)
	tun.Set("auto_route", true)
	tun.Set("strict_route", true)
	tun.Set("udp_timeout", "5m")
	packages := []any{}
	for _, name := range excludedPackages {
		if name = strings.TrimSpace(name); name != "" {
			packages = append(packages, name)
		}
	}
	if len(packages) > 0 {
		tun.Set("exclude_package", packages)
	}
	cfg.Set("inbounds", []any{tun})

	if logOptions, ok := getMap(cfg, "log"); ok {
		logOptions.Set("output", logPath("tun.log"))
	}
}

func getMap(m *jsonorder.Map, key string) (*jsonorder.Map, bool) {
	raw, ok := m.Get(key)
	if !ok {
		return nil, false
	}
	out, ok := raw.(*jsonorder.Map)
	return out, ok && out != nil
}
