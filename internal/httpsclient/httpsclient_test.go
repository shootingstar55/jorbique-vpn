package httpsclient

import (
	"crypto/tls"
	"net/http"
	"testing"
	"time"
)

func TestForHost(t *testing.T) {
	// Empty host -> client without a custom transport.
	c := ForHost("", 5*time.Second)
	if c.Timeout != 5*time.Second || c.Transport != nil {
		t.Errorf("empty host client = %+v", c)
	}

	// A host -> TLS 1.2+ against the system roots, nothing pinned.
	c = ForHost("vpn.example.com", time.Second)
	tr, ok := c.Transport.(*http.Transport)
	if !ok || tr.TLSClientConfig == nil || tr.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("transport = %+v", c.Transport)
	}
	if tr.TLSClientConfig.VerifyPeerCertificate != nil || tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("the transport must verify certificates the ordinary way")
	}
}
