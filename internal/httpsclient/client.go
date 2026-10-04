package httpsclient

import (
	"crypto/tls"
	"net"
	"net/http"
	"strings"
	"time"
)

// ForHost returns an HTTP client for fetches from host: TLS 1.2 or newer,
// verified against the system trust store. The update server sits behind a CDN
// whose certificates rotate on its own schedule, so nothing is pinned.
func ForHost(host string, timeout time.Duration) *http.Client {
	if strings.TrimSpace(host) == "" {
		return &http.Client{Timeout: timeout}
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:           http.ProxyFromEnvironment,
			DialContext:     (&net.Dialer{Timeout: 30 * time.Second}).DialContext,
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}
}
