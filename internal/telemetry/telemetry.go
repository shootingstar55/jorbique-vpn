// Package telemetry pushes client metrics over OTLP to the pinned server:
// nginx /otlp checks the UUID and names the user, otel_collector hands the
// metrics to Prometheus.
package telemetry

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"

	"jorbiquevpn/internal/constants"
	"jorbiquevpn/internal/httpsclient"
	"jorbiquevpn/internal/paths"
	"jorbiquevpn/internal/security"
)

// endpoint is a variable only so tests can point it at httptest.
var endpoint = "https://" + constants.ServerHost + "/otlp/v1/metrics"

// ErrUnknownUUID: the server answered 401, it has no user with this UUID.
var ErrUnknownUUID = errors.New("the server does not know this user UUID")

// ErrVerificationUnavailable means this server does not expose the UUID check.
var ErrVerificationUnavailable = errors.New("the server does not support UUID verification; update the server or use a compatible client")

// Start reports once a minute whether each tunnel is up. stateDir keeps this
// install's ID, so two machines of one user stay two series. userUUID is the
// user's VLESS UUID, read on every push: one entered after Start is picked up,
// and while it is empty nothing is sent. logf hears when pushing starts or
// stops failing, not every failed minute.
func Start(stateDir string, userUUID func() string, tunnels func() map[string]bool, singboxVersion func() string, logf func(string)) {
	exp, err := newExporter(&authTransport{uuid: userUUID, logf: logf}, singboxVersion)
	if err != nil {
		logf("telemetry: " + err.Error())
		return
	}
	// Push failures reach logf through authTransport, once per change.
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) {
		// authTransport already reports push failures once per state change.
	}))
	reader := sdkmetric.NewPeriodicReader(exp, sdkmetric.WithInterval(time.Minute))
	if err := observeTunnels(stateDir, reader, tunnels); err != nil {
		logf("telemetry: " + err.Error())
	}
}

// Verify pushes the current metrics once, signed with userUUID, which need not
// be saved yet, so the check and a new user's first metrics are one request.
// nil: the server knows the UUID — save it and Start's minute pushes pick it
// up. ErrUnknownUUID: no such user. Any other error: the server could not be
// reached, or did not answer from behind its UUID check, and nothing is known
// about the UUID.
func Verify(ctx context.Context, stateDir, userUUID string, tunnels func() map[string]bool, singboxVersion func() string) error {
	if !security.ValidUUID(userUUID, false) {
		return ErrUnknownUUID // never send what cannot be a UUID
	}
	// Its own transport, exporter and reader: nothing shared with Start's
	// pushes, so no lock and no "push ok/failing" log from here.
	tr := &authTransport{uuid: func() string { return userUUID }, logf: func(string) {
		// Verify returns the export error directly instead of emitting background logs.
	}}
	exp, err := newExporter(tr, singboxVersion)
	if err != nil {
		return err
	}
	reader := sdkmetric.NewManualReader()
	if err := observeTunnels(stateDir, reader, tunnels); err != nil {
		return err
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		return err
	}
	err = exp.Export(ctx, &rm)
	// The exporter's error hides the HTTP status (429 not even in the text);
	// RoundTrip ran on this goroutine and kept it.
	switch {
	case tr.status >= 200 && tr.status < 300:
		return nil
	case tr.status == http.StatusUnauthorized:
		return ErrUnknownUUID
	case tr.status == http.StatusNotFound || tr.status == http.StatusMethodNotAllowed:
		return fmt.Errorf("%w (HTTP %d)", ErrVerificationUnavailable, tr.status)
	case tr.status == http.StatusTooManyRequests || tr.status >= 500:
		// Both come from behind nginx's UUID check, which answers 401 first:
		// 429 is its rate limit, 5xx the collector being down or restarting.
		// The UUID is known; this push is lost and a later minute's carries
		// the metrics. Anything else — no answer, or a 404/405 from a server
		// without /otlp — says nothing about the UUID.
		return nil
	}
	return err
}

func newExporter(tr *authTransport, singboxVersion func() string) (sdkmetric.Exporter, error) {
	client := httpsclient.ForHost(constants.ServerHost, 30*time.Second)
	tr.next = client.Transport
	if tr.next == nil {
		// No host linked in (tests, an unconfigured build): plain default transport.
		tr.next = http.DefaultTransport
	}
	client.Transport = tr
	exp, err := otlpmetrichttp.New(context.Background(),
		otlpmetrichttp.WithEndpointURL(endpoint),
		otlpmetrichttp.WithHTTPClient(client),
		// The next minute replaces a lost one; retries would only pile up offline.
		otlpmetrichttp.WithRetry(otlpmetrichttp.RetryConfig{Enabled: false}),
	)
	if err != nil {
		return nil, err
	}
	return &deviceMetadataExporter{Exporter: exp, version: singboxVersion, name: deviceName}, nil
}

// The core can be updated and the computer renamed after Start. Keep these on
// the resource, not the data point: the collector replaces the old metadata
// without changing the identity of this device/tunnel.
type deviceMetadataExporter struct {
	sdkmetric.Exporter
	version func() string
	name    func(context.Context) string
}

func (e *deviceMetadataExporter) Export(ctx context.Context, data *metricdata.ResourceMetrics) error {
	current := *data
	attrs := append(data.Resource.Attributes(),
		attribute.String("singbox.version", knownVersion(e.version())),
		attribute.String("host.name", e.name(ctx)),
	)
	current.Resource = resource.NewSchemaless(attrs...)
	return e.Exporter.Export(ctx, &current)
}

// observeTunnels makes reader report jorbique_vpn.tunnel.up for this install.
//
// The names are the server's protocol, not the app's brand: its collector keeps
// only jorbique_vpn.* metrics, and Prometheus turns target_info from
// service.name jorbique-vpn into the device list. Renamed, every push is dropped.
func observeTunnels(stateDir string, reader sdkmetric.Reader, tunnels func() map[string]bool) error {
	osName, osVersion, osBuild := deviceOS()
	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(resource.NewSchemaless(
			attribute.String("service.name", "jorbique-vpn"),
			attribute.String("service.version", constants.AppVersion),
			attribute.String("service.instance.id", instanceID(stateDir)),
			attribute.String("os.type", runtime.GOOS),
			attribute.String("os.name", osName),
			attribute.String("os.version", knownVersion(osVersion)),
			attribute.String("os.build.id", knownVersion(osBuild)),
		)),
		sdkmetric.WithReader(reader),
	)
	_, err := provider.Meter("jorbiquevpn").Int64ObservableGauge("jorbique_vpn.tunnel.up",
		metric.WithDescription("1 while the tunnel is up, 0 while it is configured but down."),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			for name, up := range tunnels() {
				var v int64
				if up {
					v = 1
				}
				o.Observe(v, metric.WithAttributes(attribute.String("vpn", name)))
			}
			return nil
		}))
	return err
}

// instanceID returns a random ID stored in dir, made on first use. If it
// cannot be stored, each start gets a new one and the old series expires.
func instanceID(dir string) string {
	path := filepath.Join(dir, paths.TelemetryInstanceIDFileName)
	if b, err := os.ReadFile(path); err == nil && len(bytes.TrimSpace(b)) > 0 {
		return string(bytes.TrimSpace(b))
	}
	id := uuid.NewString()
	_ = os.WriteFile(path, []byte(id+"\n"), 0o644)
	return id
}

var errNoUUID = errors.New("no user UUID yet")

// authTransport signs each push with the user's UUID and logs when pushing
// starts or stops failing.
type authTransport struct {
	next http.RoundTripper
	uuid func() string
	logf func(string)

	mu      sync.Mutex
	failing bool
	status  int // of the last push, 0 if it got no answer
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var resp *http.Response
	err := errNoUUID
	// Lower case: the server's user map is, and sing-box accepts either.
	if id := strings.ToLower(strings.TrimSpace(t.uuid())); id != "" {
		req = req.Clone(req.Context())
		req.Header.Set("Authorization", "Bearer "+id)
		resp, err = t.next.RoundTrip(req)
	}
	status := 0
	if err == nil {
		status = resp.StatusCode
	}
	failing := err != nil || status >= 300
	t.mu.Lock()
	changed := failing != t.failing
	t.failing = failing
	t.status = status
	t.mu.Unlock()
	switch {
	case changed && err != nil:
		t.logf("telemetry: push failing: " + err.Error())
	case changed && failing:
		t.logf(fmt.Sprintf("telemetry: push failing: HTTP %d", status))
	case changed:
		t.logf("telemetry: push ok")
	}
	return resp, err
}
