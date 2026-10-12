package vless

import (
	"testing"
)

const vlessUUID = "22222222-2222-2222-2222-222222222222"

func TestParseVlessGrpc_WithTLS(t *testing.T) {
	p, err := ParseVlessGrpc("vless://" + vlessUUID + "@example.com:443?security=tls&servicename=Svc&sni=grpc.host#x")
	if err != nil || p == nil {
		t.Fatalf("parse failed: %v", err)
	}
	if p.Security != "tls" || p.ServerName != "grpc.host" || p.ServiceName != "Svc" {
		t.Errorf("grpc tls fields: %+v", p)
	}

	// security=tls without sni -> ServerName falls back to server.
	p2, _ := ParseVlessGrpc("vless://" + vlessUUID + "@host.example:443?security=tls")
	if p2.ServerName != "host.example" {
		t.Errorf("sni fallback = %q", p2.ServerName)
	}

	// Default service name when omitted and no TLS.
	p3, _ := ParseVlessGrpc("vless://" + vlessUUID + "@host.example:443")
	if p3.ServiceName != "MediaService" || p3.Security != "" {
		t.Errorf("defaults: %+v", p3)
	}
}

func TestParseAnyTLS_Errors(t *testing.T) {
	if _, err := ParseAnyTLS("http://x"); err != ErrInvalidURL {
		t.Errorf("wrong scheme err = %v", err)
	}
	if _, err := ParseAnyTLS("anytls://@h:443"); err != ErrInvalidURL {
		t.Errorf("empty password should error, got %v", err)
	}
	// URL-escaped password is unescaped.
	p, err := ParseAnyTLS("anytls://p%40ss@example.com:443?sni=a.host")
	if err != nil || p.Password != "p@ss" || p.ServerName != "a.host" {
		t.Errorf("anytls parse: %+v %v", p, err)
	}
}

func TestParseHostPort(t *testing.T) {
	cases := []struct {
		in     string
		server string
		port   int
		ok     bool
	}{
		{"example.com:443", "example.com", 443, true},
		{"[2001:db8::1]:8443", "2001:db8::1", 8443, true},
		{"nohost", "", 0, false},
		{"host:notaport", "", 0, false},
		{"host:0", "", 0, false},
		{"[2001:db8::1]", "", 0, false}, // missing ]:
		{"[2001:db8::1]:bad", "", 0, false},
	}
	for _, c := range cases {
		srv, port, err := parseHostPort(c.in)
		if c.ok && (err != nil || srv != c.server || port != c.port) {
			t.Errorf("parseHostPort(%q) = %q,%d,%v", c.in, srv, port, err)
		}
		if !c.ok && err == nil {
			t.Errorf("parseHostPort(%q) expected error", c.in)
		}
	}
}

func TestServerName(t *testing.T) {
	if serverName("srv", "sni") != "sni" {
		t.Error("sni should win when present")
	}
	if serverName("srv", "") != "srv" {
		t.Error("server should be used when sni empty")
	}
}
