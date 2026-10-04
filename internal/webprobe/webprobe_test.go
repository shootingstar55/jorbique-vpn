package webprobe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGetReturnsTheBodyAndStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("203.0.113.7\n"))
	}))
	defer srv.Close()

	res, err := Get(context.Background(), srv.URL, Options{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if res.Status != 200 || strings.TrimSpace(res.Body) != "203.0.113.7" {
		t.Errorf("got %d %q", res.Status, res.Body)
	}
	if !res.OK() {
		t.Error("200 is not OK")
	}
}

// curl --fail: a service answering 5xx is one to skip, not an error page to
// hand to the IP parsers.
func TestFailOnHTTPErrorTurnsAnErrorStatusIntoAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("<html>down for maintenance</html>"))
	}))
	defer srv.Close()

	res, err := Get(context.Background(), srv.URL, Options{FailOnHTTPError: true})
	if err == nil {
		t.Fatal("a 503 was accepted")
	}
	if res.Status != 503 {
		t.Errorf("status = %d", res.Status)
	}

	// Without the flag the caller decides, which is what the primary URL does.
	res, err = Get(context.Background(), srv.URL, Options{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if res.OK() {
		t.Error("503 reported as usable")
	}
}

// curl --location. The connectivity check depends on it: gitlab answers a
// 301 to https, and stopping there would call the tunnel healthy on the
// redirect alone.
func TestRedirectsAreFollowed(t *testing.T) {
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("arrived"))
	}))
	defer final.Close()
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL, http.StatusMovedPermanently)
	}))
	defer first.Close()

	res, err := Get(context.Background(), first.URL, Options{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if res.Body != "arrived" {
		t.Errorf("body = %q, want the redirect target's", res.Body)
	}
}

// HEAD is what the connectivity listing uses: it wants the status and nothing
// else, and reading a body it will not look at is wasted on a slow link.
func TestHeadAsksForNoBody(t *testing.T) {
	var method string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		_, _ = w.Write([]byte("body"))
	}))
	defer srv.Close()

	res, err := Get(context.Background(), srv.URL, Options{Method: http.MethodHead})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if method != http.MethodHead {
		t.Errorf("server saw %s", method)
	}
	if res.Body != "" {
		t.Errorf("body = %q, want none", res.Body)
	}
}

// curl --max-time: the deadline covers the whole request, not just connecting.
// A service that accepts and then goes quiet used to hold the exit-IP check
// for as long as it felt like.
func TestTheTimeoutCoversAStalledResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer srv.Close()

	start := time.Now()
	if _, err := Get(context.Background(), srv.URL, Options{Timeout: 200 * time.Millisecond}); err == nil {
		t.Fatal("a stalled response was accepted")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("waited %v, well past the timeout", elapsed)
	}
}

// A body that never ends must not be read into memory forever: some of these
// endpoints are behind captive portals that stream.
func TestTheBodyIsBounded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := strings.Repeat("x", 8<<10)
		for i := 0; i < 64; i++ {
			if _, err := w.Write([]byte(chunk)); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	res, err := Get(context.Background(), srv.URL, Options{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(res.Body) > maxBody {
		t.Errorf("read %d bytes, cap is %d", len(res.Body), maxBody)
	}
}

// A connection that goes away part-way through the body is what curl's exit
// status used to catch. Half an address parses into a plausible wrong one, so
// the caller has to hear about it and try the next service.
func TestATruncatedBodyIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "64")
		_, _ = w.Write([]byte("203.0.113"))
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	}))
	defer srv.Close()

	res, err := Get(context.Background(), srv.URL, Options{})
	if err == nil {
		t.Fatalf("a truncated body was accepted, body %q", res.Body)
	}
	if res.Body != "" {
		t.Errorf("the bytes that did arrive were handed over anyway: %q", res.Body)
	}
}

// One client per transport shape, so a poll on an interval reuses its
// connection instead of opening and abandoning a pool per request.
func TestClientsAreReusedPerTransportShape(t *testing.T) {
	plain := clientFor(Options{}, time.Second)
	if head := clientFor(Options{Method: http.MethodHead}, time.Second); head != plain {
		t.Error("the method changed the client, and it does not belong in the key")
	}
	if v4 := clientFor(Options{IPv4Only: true}, time.Second); v4 == plain {
		t.Error("IPv4Only shares a client with the default dialer")
	}
	if slow := clientFor(Options{}, 2*time.Second); slow == plain {
		t.Error("the timeout is on the client, so it cannot be shared across timeouts")
	}
}
