package vless

import (
	"testing"
)

const vlessUUID = "22222222-2222-2222-2222-222222222222"

func TestParseHysteria2_SinglePort(t *testing.T) {
	p, err := ParseHysteria2("hysteria2://" + vlessUUID + "@example.com:443?sni=host#tag")
	if err != nil || p == nil {
		t.Fatalf("parse failed: %v", err)
	}
	if p.Server != "example.com" || p.ServerPort != 443 {
		t.Errorf("server/port = %s:%d", p.Server, p.ServerPort)
	}
	if p.Password != vlessUUID || p.UUID != vlessUUID {
		t.Errorf("password/uuid = %q/%q", p.Password, p.UUID)
	}
	if p.ServerName != "host" {
		t.Errorf("sni = %q", p.ServerName)
	}
}

func TestParseHysteria2_PortList(t *testing.T) {
	p, err := ParseHysteria2("hysteria2://" + vlessUUID + "@example.com:443,2080-3000/")
	if err != nil || p == nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(p.ServerPorts) != 2 || p.ServerPort != 0 {
		t.Errorf("ports = %v (single port %d)", p.ServerPorts, p.ServerPort)
	}
	// SNI absent -> falls back to server name.
	if p.ServerName != "example.com" {
		t.Errorf("sni fallback = %q", p.ServerName)
	}
}

func TestParseHysteria2_IPv6AndErrors(t *testing.T) {
	p, err := ParseHysteria2("hysteria2://" + vlessUUID + "@[2001:db8::1]:8443")
	if err != nil || p == nil || p.Server != "2001:db8::1" || p.ServerPort != 8443 {
		t.Fatalf("ipv6 parse: %+v err=%v", p, err)
	}
	// Wrong scheme -> (nil, nil).
	if p, err := ParseHysteria2("vless://x@h:1"); p != nil || err != nil {
		t.Errorf("non-hysteria2 scheme: %+v %v", p, err)
	}
	// Bad UUID -> (nil, nil).
	if p, _ := ParseHysteria2("hysteria2://not-a-uuid@h:443"); p != nil {
		t.Error("bad UUID should yield nil")
	}
}

func TestParseTuic(t *testing.T) {
	u := "tuic://" + vlessUUID + ":secret@example.com:443?" +
		"congestion_control=bbr&udp_relay_mode=quic&alpn=h3,h2&sni=tuic.host#name"
	p, err := ParseTuic(u)
	if err != nil || p == nil {
		t.Fatalf("parse failed: %v", err)
	}
	if p.Password != "secret" || p.UUID != vlessUUID {
		t.Errorf("creds = %q/%q", p.UUID, p.Password)
	}
	if p.ServerName != "tuic.host" {
		t.Errorf("sni = %q", p.ServerName)
	}

	// Wrong scheme -> (nil, nil); bad host:port -> error.
	if p, err := ParseTuic("vless://x@h:1"); p != nil || err != nil {
		t.Errorf("non-tuic scheme: %+v %v", p, err)
	}
	if _, err := ParseTuic("tuic://" + vlessUUID + ":pw@badhostnoport"); err == nil {
		t.Error("missing port should error")
	}
}

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
