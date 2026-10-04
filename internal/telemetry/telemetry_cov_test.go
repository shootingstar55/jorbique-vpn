package telemetry

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"jorbiquevpn/internal/paths"
)

func TestWindowsNamesOlderReleases(t *testing.T) {
	for _, tc := range []struct {
		major, minor uint32
		want         string
	}{
		{6, 2, "Windows 8"},
		{6, 1, "Windows 7"},
		{6, 0, "Windows 6.0"},
		{5, 1, "Windows 5.1"},
	} {
		if got := windowsName(tc.major, tc.minor, 0, true); got != tc.want {
			t.Errorf("%d.%d: %q, want %q", tc.major, tc.minor, got, tc.want)
		}
	}
}

// pointEndpointAt swaps the push endpoint for the length of one test.
func pointEndpointAt(t *testing.T, url string) {
	t.Helper()
	old := endpoint
	endpoint = url
	t.Cleanup(func() { endpoint = old })
}

// trustCertificateFromEnv makes the OTLP exporter read a CA from the
// environment, which it refuses to combine with a cleartext endpoint.
func trustCertificateFromEnv(t *testing.T) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "telemetry test CA"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IsCA:         true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_CERTIFICATE", path)
}

// An exporter that cannot be built is reported and nothing is observed: Start
// logs it once, Verify returns it rather than claiming anything about the UUID.
func TestExporterSetupFailureIsReported(t *testing.T) {
	trustCertificateFromEnv(t)
	pointEndpointAt(t, "http://127.0.0.1:1/otlp/v1/metrics")
	tunnels := func() map[string]bool { return map[string]bool{"sing-box": true} }
	version := func() string { return "1.14.0" }

	dir := t.TempDir()
	var logs []string
	Start(dir, func() string { return uuid.NewString() }, tunnels, version, func(s string) { logs = append(logs, s) })
	if len(logs) != 1 || !strings.HasPrefix(logs[0], "telemetry: insecure HTTP endpoint") {
		t.Fatalf("Start logged %q, want the exporter error once", logs)
	}
	if _, err := os.Stat(filepath.Join(dir, paths.TelemetryInstanceIDFileName)); !os.IsNotExist(err) {
		t.Errorf("Start went on to observe tunnels: %v", err)
	}

	err := Verify(context.Background(), dir, uuid.NewString(), tunnels, version)
	if err == nil || errors.Is(err, ErrUnknownUUID) || !strings.Contains(err.Error(), "insecure HTTP endpoint") {
		t.Fatalf("Verify = %v, want the exporter error", err)
	}
}

// A cancelled check sends nothing and says nothing about the UUID.
func TestVerifyCancelledBeforeThePush(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
	}))
	defer srv.Close()
	pointEndpointAt(t, srv.URL+"/otlp/v1/metrics")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Verify(ctx, t.TempDir(), uuid.NewString(), func() map[string]bool { return nil }, func() string { return "" })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Verify = %v, want context.Canceled", err)
	}
	if n := requests.Load(); n != 0 {
		t.Errorf("%d requests sent", n)
	}
}

// Start sets up the minute pushes without sending anything yet: the install
// gets its ID now and nothing is logged until a push succeeds or fails.
func TestStartRegistersWithoutPushing(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
	}))
	defer srv.Close()
	pointEndpointAt(t, srv.URL+"/otlp/v1/metrics")

	dir := t.TempDir()
	var logs atomic.Int32
	Start(dir, func() string { return "" }, func() map[string]bool { return nil }, func() string { return "" },
		func(string) { logs.Add(1) })
	if n := logs.Load(); n != 0 {
		t.Errorf("Start logged %d times", n)
	}
	if id, err := os.ReadFile(filepath.Join(dir, paths.TelemetryInstanceIDFileName)); err != nil || strings.TrimSpace(string(id)) != instanceID(dir) {
		t.Errorf("instance ID not stored: %q, %v", id, err)
	}
	if n := requests.Load(); n != 0 {
		t.Errorf("Start pushed %d times before the first minute", n)
	}
}
