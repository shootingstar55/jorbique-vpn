package autoconnect

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The real Extended engine routes an authenticated local SOCKS inbound through
// a local CONNECT proxy to a trusted TLS server. No public server or UUID is
// required. Tests exercise process lifecycle, remote DNS, TLS, full payloads,
// auth refusal, a 16-KiB truncation, and per-protocol failure isolation.
func TestRunnerRealEngineTransfersAndRejectsBrokenConnections(t *testing.T) {
	binary := os.Getenv("SINGBOX_BIN")
	if binary == "" {
		t.Skip("SINGBOX_BIN not set")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	var truncate atomic.Bool
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Accept-Encoding") != "" {
			t.Error("speed test must not use compression")
		}
		size := PayloadBytes
		if truncate.Load() {
			size = 16 * 1024
		}
		_, _ = w.Write([]byte(strings.Repeat("x", size)))
	}))
	defer target.Close()
	dnsName := target.Certificate().DNSNames[0]
	_, targetPort, _ := net.SplitHostPort(target.Listener.Addr().String())
	remoteTarget := net.JoinHostPort(dnsName, targetPort)
	var connects atomic.Int32
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "CONNECT required", 405)
			return
		}
		// One outbound is intentionally refused; the other carries the verified UUID.
		if r.Header.Get("Proxy-Authorization") != "Basic Y2hlY2s6MTExMTExMTEtMTExMS00MTExLTgxMTEtMTExMTExMTExMTEx" {
			http.Error(w, "denied", 407)
			return
		}
		connects.Add(1)
		if r.Host != remoteTarget {
			t.Errorf("target DNS did not travel through SOCKS/CONNECT: %s", r.Host)
			http.Error(w, "wrong target", 400)
			return
		}
		upstream, err := net.DialTimeout("tcp", target.Listener.Addr().String(), time.Second)
		if err != nil {
			http.Error(w, "unreachable", 502)
			return
		}
		downstream, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			upstream.Close()
			return
		}
		defer downstream.Close()
		defer upstream.Close()
		_, _ = buf.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = buf.Flush()
		done := make(chan struct{})
		go func() { _, _ = io.Copy(upstream, buf); close(done) }()
		_, _ = io.Copy(downstream, upstream)
		downstream.Close()
		upstream.Close()
		<-done
	}))
	defer proxyServer.Close()
	host, portText, _ := net.SplitHostPort(strings.TrimPrefix(proxyServer.URL, "http://"))
	port, _ := strconv.Atoi(portText)
	raw, _ := json.Marshal(map[string]any{"outbounds": []any{
		map[string]any{"type": "http", "tag": "works", "server": host, "server_port": port, "username": "check", "password": "__JORBIQUE_USER_UUID__"},
		map[string]any{"type": "http", "tag": "refused", "server": host, "server_port": port, "username": "check", "password": "wrong"},
		map[string]any{"type": "urltest", "tag": "auto", "outbounds": []string{"works", "refused"}},
	}, "route": map[string]any{"final": "auto"}})
	runner, err := NewRunner(binary, filepath.Dir(binary), raw, "11111111-1111-4111-8111-111111111111")
	if err != nil {
		t.Fatal(err)
	}
	dir := runner.dir
	defer runner.Close()
	runner.target = "https://" + remoteTarget
	runner.tlsTransport = target.Client().Transport.(*http.Transport)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	measurement, err := runner.Probe(ctx, "works")
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Bytes != 2*PayloadBytes || requests.Load() != 2 || connects.Load() == 0 {
		t.Fatalf("measurement=%+v requests=%d connects=%d", measurement, requests.Load(), connects.Load())
	}
	if _, err := os.Stat(filepath.Join(dir, "probe.json")); !os.IsNotExist(err) {
		t.Fatal("private probe config not removed")
	}
	if _, err := runner.Probe(ctx, "refused"); err == nil {
		t.Fatal("authentication refusal counted as success")
	}
	truncate.Store(true)
	if _, err := runner.Probe(ctx, "works"); err == nil {
		t.Fatal("16-KiB partial transfer counted as success")
	}
	truncate.Store(false)
	if _, err := runner.Probe(ctx, "works"); err != nil {
		t.Fatalf("failed candidate poisoned next probe: %v", err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, err := runner.Probe(canceled, "works"); err == nil {
		t.Fatal("canceled probe succeeded")
	}
	runner.Close()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("probe directory survived cleanup")
	}
	if filepath.Base(runner.binary) == filepath.Base(binary) {
		t.Fatal(fmt.Sprintf("probe uses tunnel's process name: %s", runner.binary))
	}
}
