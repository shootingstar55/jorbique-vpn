package telemetry

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestInstanceIDSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	first := instanceID(dir)
	if first == "" || instanceID(dir) != first {
		t.Fatalf("instance ID changed between starts: %q", first)
	}
	if instanceID(t.TempDir()) == first {
		t.Fatal("two installs got the same instance ID")
	}
}

func TestAuthTransportSignsPushesAndLogsOnlyChanges(t *testing.T) {
	var gotAuth []string
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = append(gotAuth, r.Header.Get("Authorization"))
		w.WriteHeader(status)
	}))
	defer srv.Close()

	userUUID := ""
	var logs []string
	tr := &authTransport{next: http.DefaultTransport, uuid: func() string { return userUUID }, logf: func(s string) { logs = append(logs, s) }}
	push := func() {
		req, _ := http.NewRequest(http.MethodPost, srv.URL, nil)
		if resp, err := tr.RoundTrip(req); err == nil {
			resp.Body.Close()
		}
	}

	push() // no UUID: nothing leaves the machine
	push()
	id := uuid.NewString()
	userUUID = " " + strings.ToUpper(id) + " "
	push()
	push()
	status = http.StatusUnauthorized
	push()

	wantAuth := []string{"Bearer " + id, "Bearer " + id, "Bearer " + id}
	if !reflect.DeepEqual(gotAuth, wantAuth) {
		t.Fatalf("Authorization sent = %q, want %q", gotAuth, wantAuth)
	}
	wantLogs := []string{"telemetry: push failing: no user UUID yet", "telemetry: push ok", "telemetry: push failing: HTTP 401"}
	if !reflect.DeepEqual(logs, wantLogs) {
		t.Fatalf("logs = %q, want %q", logs, wantLogs)
	}
}

func TestVerifyTellsUnknownUUIDFromUnreachableServer(t *testing.T) {
	var gotAuth []string
	var gotBody []byte
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = append(gotAuth, r.Header.Get("Authorization"))
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(status)
	}))
	defer srv.Close()
	defer func(old string) { endpoint = old }(endpoint)
	endpoint = srv.URL + "/otlp/v1/metrics"

	dir := t.TempDir()
	tunnels := func() map[string]bool { return map[string]bool{"sing-box": false} }
	id := uuid.NewString()
	for _, tc := range []struct {
		status int
		want   error
	}{
		{http.StatusOK, nil},
		{http.StatusUnauthorized, ErrUnknownUUID},
		{http.StatusNotFound, ErrVerificationUnavailable},
		{http.StatusMethodNotAllowed, ErrVerificationUnavailable},
		// A 429 or a 5xx comes only after nginx has matched the UUID.
		{http.StatusTooManyRequests, nil},
		{http.StatusBadGateway, nil},
	} {
		status = tc.status
		if err := Verify(context.Background(), dir, strings.ToUpper(id), tunnels, func() string { return "1.14.0-alpha.2" }); !errors.Is(err, tc.want) {
			t.Fatalf("HTTP %d: Verify = %v, want %v", tc.status, err, tc.want)
		}
	}
	// The push carried the metrics, signed with the candidate, under this install's ID.
	if !bytes.Contains(gotBody, []byte("jorbique_vpn.tunnel.up")) || !bytes.Contains(gotBody, []byte(instanceID(dir))) {
		t.Fatalf("push body lacks the gauge or the instance ID: %q", gotBody)
	}
	if want := "Bearer " + id; gotAuth[0] != want {
		t.Fatalf("Authorization = %q, want %q", gotAuth[0], want)
	}

	// A server refusal is neither a known UUID nor an unavailable verification route.
	status = http.StatusForbidden
	if err := Verify(context.Background(), dir, id, tunnels, func() string { return "1.14.0-alpha.2" }); err == nil || errors.Is(err, ErrUnknownUUID) || errors.Is(err, ErrVerificationUnavailable) {
		t.Fatalf("HTTP 403: Verify = %v, want a plain error", err)
	}
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	endpoint = "http://" + l.Addr().String() + "/otlp/v1/metrics"
	l.Close()
	err := Verify(context.Background(), dir, id, tunnels, func() string { return "1.14.0-alpha.2" })
	var urlErr *url.Error
	if err == nil || errors.Is(err, ErrUnknownUUID) || errors.Is(err, ErrVerificationUnavailable) || !errors.As(err, &urlErr) {
		t.Fatalf("closed port: Verify = %v, want a *url.Error", err)
	}

	// Not a UUID: refused here, nothing leaves the machine.
	n := len(gotAuth)
	if err := Verify(context.Background(), dir, "my-password", tunnels, func() string { return "1.14.0-alpha.2" }); !errors.Is(err, ErrUnknownUUID) || len(gotAuth) != n {
		t.Fatalf("malformed: Verify = %v, requests %d -> %d", err, n, len(gotAuth))
	}
}
