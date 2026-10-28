package httpsclient

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
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
