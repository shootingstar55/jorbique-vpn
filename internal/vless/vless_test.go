package vless

import "testing"

func TestParseVlessGrpc(t *testing.T) {
	parsed, err := ParseVlessGrpc("vless://550e8400-e29b-48d4-8716-446655440000@1.2.3.4:8443?type=grpc&serviceName=MediaService#VLESSgRPC")
	if err != nil || parsed == nil {
		t.Fatal("Valid gRPC URL should parse", err)
	}
	if parsed.Server != "1.2.3.4" || parsed.ServerPort != 8443 || parsed.ServiceName != "MediaService" {
		t.Error("Parsed gRPC values incorrect")
	}
	if _, err := ParseVlessGrpc("https://example.com"); err == nil {
		t.Error("Non-vless should fail")
	}
}

func TestParseAnyTLS(t *testing.T) {
	u := "anytls://11111111-2222-3333-4444-555555555555@tls.example.com:443?sni=tls.example.com#anytls"
	p, err := ParseAnyTLS(u)
	if err != nil || p == nil {
		t.Fatal("Valid AnyTLS URL should parse", err)
	}
	if p.Password != "11111111-2222-3333-4444-555555555555" || p.Server != "tls.example.com" || p.ServerPort != 443 {
		t.Error("Parsed host/password incorrect", p)
	}
	if p.ServerName != "tls.example.com" {
		t.Error("SNI / server_name incorrect", p.ServerName)
	}
	if _, err := ParseAnyTLS("vless://x@y:443"); err == nil {
		t.Error("Non-anytls scheme should fail")
	}
}
