package httpsclient

import (
	"crypto/tls"
	"net"
	"net/http"
	"strings"
	"time"

	"jorbiquevpn/internal/constants"
)

// VersionHeader carries the client version on every request to the server:
// the server answers a client too old for its protocols with an update notice
// instead of a profile, and a request without the header is such a client.
const VersionHeader = "X-Jorbique-Version"

// ForHost returns an HTTP client for fetches from host: TLS 1.2 or newer,
// verified against the system trust store. The update server sits behind a CDN
// whose certificates rotate on its own schedule, so nothing is pinned.
func ForHost(host string, timeout time.Duration) *http.Client {
	if strings.TrimSpace(host) == "" {
		return &http.Client{Timeout: timeout}
	}
	return &http.Client{
		Timeout: timeout,
		Transport: versioned{&http.Transport{
			Proxy:           http.ProxyFromEnvironment,
			DialContext:     (&net.Dialer{Timeout: 30 * time.Second}).DialContext,
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		}},
	}
}

// versioned sets VersionHeader on each request; redirects are new requests
// and pass through it too.
type versioned struct{ base *http.Transport }

func (v versioned) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set(VersionHeader, constants.AppVersion)
	return v.base.RoundTrip(req)
}
