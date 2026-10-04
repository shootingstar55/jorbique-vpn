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

// Options the server may add to the URL are ignored, not refused: the client
// takes the credential out of it and the template carries everything else.
func TestParseTuicIgnoresTheOptionalQuery(t *testing.T) {
	u := "tuic://7998568d-7ae4-4f65-a4cb-b20e78af6a87:testpass@cdn.example.com:443?alpn=h3&congestion_control=bbr&udp_relay_mode=native&sni=cdn.example.com#name-tuic"
	p, err := ParseTuic(u)
	if err != nil || p == nil {
		t.Fatal("Valid TUIC URL should parse", err)
	}
	if p.Server != "cdn.example.com" || p.ServerPort != 443 || p.Password != "testpass" {
		t.Errorf("host/password: %+v", p)
	}
	if p.UUID != "7998568d-7ae4-4f65-a4cb-b20e78af6a87" || p.ServerName != "cdn.example.com" {
		t.Errorf("uuid/sni: %+v", p)
	}
}

func TestParseAnyTLS(t *testing.T) {
	u := "anytls://e97a0972-0ca0-43b9-ba24-5f52f1c63f4f@tls.edge-nl-01.jorbique.one:443?sni=tls.edge-nl-01.jorbique.one#anytls"
	p, err := ParseAnyTLS(u)
	if err != nil || p == nil {
		t.Fatal("Valid AnyTLS URL should parse", err)
	}
	if p.Password != "e97a0972-0ca0-43b9-ba24-5f52f1c63f4f" || p.Server != "tls.edge-nl-01.jorbique.one" || p.ServerPort != 443 {
		t.Error("Parsed host/password incorrect", p)
	}
	if p.ServerName != "tls.edge-nl-01.jorbique.one" {
		t.Error("SNI / server_name incorrect", p.ServerName)
	}
	if _, err := ParseAnyTLS("vless://x@y:443"); err == nil {
		t.Error("Non-anytls scheme should fail")
	}
}
