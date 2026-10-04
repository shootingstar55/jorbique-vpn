// Package webprobe makes the HTTP requests this client used to make by running
// /usr/bin/curl.
//
// curl was there for a reason that no longer holds. When the menu bar ran as
// root — which it did, via sudo — a request from our own process did not
// necessarily leave the machine the way the user's browser traffic did, so the
// exit-IP check shelled out to curl and went to some trouble to run it in the
// console user's session: launchctl asuser, then sudo -u, then plain root as a
// last resort. The bar runs as the logged-in user now, and the daemon's checks
// are about the tunnel rather than about one user's session, so the ladder has
// nothing left to solve. What remains is an HTTP GET, which Go does.
//
// Behaviour is kept where it mattered: curl's --fail (4xx and 5xx are a
// failure, not a body to parse), --location (follow redirects), --max-time (a
// deadline for the whole request, not just the connection) and -4 (IPv4 only,
// so an IPv6-capable link does not report an IPv6 exit address the user cannot
// compare with what a v4-only service shows them).
package webprobe

import (
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// maxBody bounds what a probe reads. These endpoints answer with an IP address
// or a small JSON object; anything larger is a captive portal or an error page,
// and reading it in full helps nobody.
const maxBody = 64 << 10

// Options is what the curl flags used to say.
type Options struct {
	// Method defaults to GET. HEAD is used by the connectivity check, which
	// only wants the status.
	Method string
	// Timeout bounds the whole request. Zero means 8 seconds, curl's
	// --max-time in the callers that had one.
	Timeout time.Duration
	// IPv4Only dials A records only (curl -4).
	IPv4Only bool
	// FailOnHTTPError makes a 4xx or 5xx an error rather than a body (curl -f).
	FailOnHTTPError bool
}

// Result is what the callers used to read off curl's stdout and exit status.
type Result struct {
	Status int
	Body   string
}

// OK reports a response the caller can use: a status curl would not have
// failed on.
func (r Result) OK() bool { return r.Status > 0 && r.Status < 400 }

// Get performs the request. The error is for "no answer at all" — DNS, connect,
// timeout, TLS; an HTTP error status comes back in Result unless
// FailOnHTTPError asked otherwise.
func Get(ctx context.Context, url string, opt Options) (Result, error) {
	timeout := opt.Timeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	method := strings.TrimSpace(opt.Method)
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return Result{}, err
	}

	resp, err := clientFor(opt, timeout).Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()

	res := Result{Status: resp.StatusCode}
	if method != http.MethodHead {
		// A body that stopped early is not a shorter answer, it is no answer:
		// half an IP address parses into a plausible-looking wrong one, which is
		// what curl's exit status used to keep out. The caller wants the next
		// service in its list, so the read error is returned rather than the
		// bytes that arrived before the connection went away. Hitting maxBody is
		// not this case — LimitReader ends in EOF, which ReadAll reports as
		// success.
		body, rerr := io.ReadAll(io.LimitReader(resp.Body, maxBody))
		if rerr != nil {
			return res, rerr
		}
		res.Body = string(body)
	}
	if opt.FailOnHTTPError && !res.OK() {
		return res, &StatusError{Status: res.Status, URL: url}
	}
	return res, nil
}

// StatusError is curl's --fail: the server answered, and the answer is not one
// to work with.
type StatusError struct {
	Status int
	URL    string
}

func (e *StatusError) Error() string {
	return "http " + strconv.Itoa(e.Status) + " from " + e.URL
}

// clients caches one client per distinct transport shape. The connectivity
// check polls the same host on an interval, and a fresh Transport per call
// means a fresh connection pool per call: no reuse, and idle connections left
// to time out on their own. The key holds only what the transport is built
// from — Method is a per-request choice and does not belong in it.
var clients sync.Map

type clientKey struct {
	timeout  time.Duration
	ipv4Only bool
}

func clientFor(opt Options, timeout time.Duration) *http.Client {
	key := clientKey{timeout: timeout, ipv4Only: opt.IPv4Only}
	if c, ok := clients.Load(key); ok {
		return c.(*http.Client)
	}
	c, _ := clients.LoadOrStore(key, newClient(opt, timeout))
	return c.(*http.Client)
}

func newClient(opt Options, timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	tr := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		TLSHandshakeTimeout: timeout,
		DialContext:         dialer.DialContext,
	}
	if opt.IPv4Only {
		tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			switch network {
			case "tcp", "tcp4", "tcp6":
				network = "tcp4"
			}
			return dialer.DialContext(ctx, network, addr)
		}
	}
	// Redirects are followed, as curl --location did. The default limit of ten
	// is more than any of these endpoints uses.
	return &http.Client{Transport: tr, Timeout: timeout}
}
