package httpsclient

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"jorbiquevpn/internal/constants"
)

func TestForHost(t *testing.T) {
	// Empty host -> client without a custom transport.
	c := ForHost("", 5*time.Second)
	if c.Timeout != 5*time.Second || c.Transport != nil {
		t.Errorf("empty host client = %+v", c)
	}

	// A host -> TLS 1.2+ against the system roots, nothing pinned.
	c = ForHost("vpn.example.com", time.Second)
	v, ok := c.Transport.(versioned)
	if !ok {
		t.Fatalf("transport = %+v", c.Transport)
	}
	tr := v.base
	if tr.TLSClientConfig == nil || tr.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("transport = %+v", c.Transport)
	}
	if tr.TLSClientConfig.VerifyPeerCertificate != nil || tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("the transport must verify certificates the ordinary way")
	}
}

func TestRedirectBoundaries(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		t.Run(fmt.Sprint(authenticated), func(t *testing.T) {
			for _, downgrade := range []bool{false, true} {
				called := false
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
				var target *httptest.Server
				if downgrade {
					target = httptest.NewServer(handler)
				} else {
					target = httptest.NewTLSServer(handler)
				}
				defer target.Close()
				origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/same-origin" {
						w.WriteHeader(http.StatusNoContent)
						return
					}
					location := target.URL
					if r.URL.Path == "/start" {
						location = "/same-origin"
					}
					http.Redirect(w, r, location, http.StatusTemporaryRedirect)
				}))
				defer origin.Close()
				client := ForHost("127.0.0.1", time.Second)
				client.Transport.(versioned).base.TLSClientConfig.RootCAs = origin.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
				for _, path := range []string{"/start", "/cross-origin"} {
					req, _ := http.NewRequest(http.MethodGet, origin.URL+path, nil)
					if authenticated {
						req.Header.Set("Authorization", "Bearer secret")
					}
					resp, err := client.Do(req)
					if resp != nil {
						resp.Body.Close()
					}
					blocked := path == "/cross-origin" && (downgrade || authenticated)
					if blocked && (err == nil || !strings.Contains(err.Error(), "refusing")) {
						t.Fatalf("redirect must be refused: %v", err)
					}
					if !blocked && err != nil {
						t.Fatal(err)
					}
					if blocked && called {
						t.Fatal("redirect target received the request")
					}
				}
			}
		})
	}
}

func TestForHostSendsVersion(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(VersionHeader)
	}))
	defer srv.Close()
	resp, err := ForHost("127.0.0.1", time.Second).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got != constants.AppVersion {
		t.Errorf("%s = %q, want %q", VersionHeader, got, constants.AppVersion)
	}
}
