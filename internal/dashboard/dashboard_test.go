package dashboard

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"jorbiquevpn/internal/jsonorder"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/protoadapt"
)

func TestConfigureAPIUsesLoopbackSecretAndNoBundledDashboard(t *testing.T) {
	root := jsonorder.FromMap(map[string]any{
		"services": []any{jsonorder.FromMap(map[string]any{
			"type": "api", "listen": "0.0.0.0", "dashboard": jsonorder.FromMap(map[string]any{"enabled": true}),
		})},
		"http_clients": []any{jsonorder.FromMap(map[string]any{"tag": "dashboard-http"})},
	})
	if err := ConfigureAPI(root, ""); err != nil {
		t.Fatal(err)
	}
	services, _ := root.Get("services")
	api := services.([]any)[0].(*jsonorder.Map)
	if api.GetString("listen") != "127.0.0.1" || api.GetString("secret") == "" {
		t.Fatalf("insecure API service: %v", api)
	}
	if port, _ := api.Get("listen_port"); port != Port {
		t.Fatalf("listen_port = %v, want %d", port, Port)
	}
	if api.Has("dashboard") || root.Has("http_clients") {
		t.Fatalf("downloadable dashboard remains: %v", root)
	}
}

func TestConfigureAPIPreservesLocalSecret(t *testing.T) {
	root := jsonorder.New()
	if err := ConfigureAPI(root, "stable-secret"); err != nil {
		t.Fatal(err)
	}
	if got := APISecret(root); got != "stable-secret" {
		t.Fatalf("API secret = %q, want stable-secret", got)
	}
}

func TestLocalServerServesOnlyTokenScopedAssets(t *testing.T) {
	assets := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("jorbique-dashboard")}}
	server, err := StartLocalServer("dashboard=1", "unused.json", fs.FS(assets), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/api/snapshot"} {
		response, err := http.Get(serverURL.Scheme + "://" + serverURL.Host + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("unscoped %s status = %d, want 404", path, response.StatusCode)
		}
	}
	response, err := http.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || string(body) != "jorbique-dashboard" {
		t.Fatalf("dashboard response = %s %q", response.Status, body)
	}
}

func TestConfigServerBridgesSharedForm(t *testing.T) {
	assets := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("config")}}
	state := ConfigState{
		UUID:               "7998568d-7ae4-4f65-a4cb-b20e78af6a87",
		AvailableOutbounds: []string{"vless-reality", "tuic"},
		OutboundEnabled:    map[string]bool{"vless-reality": true, "tuic": false},
	}
	var saved ConfigUpdate
	closed := make(chan struct{}, 1)
	server, err := StartConfigServer("vpn-config=1", state, fs.FS(assets), func(update ConfigUpdate) error {
		saved = update
		return nil
	}, func() { closed <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	base := serverURL.Scheme + "://" + serverURL.Host + serverURL.Path + "api/"

	response, err := http.Get(base + "config")
	if err != nil {
		t.Fatal(err)
	}
	var got ConfigState
	err = json.NewDecoder(response.Body).Decode(&got)
	response.Body.Close()
	if err != nil || got.UUID != state.UUID || len(got.AvailableOutbounds) != 2 {
		t.Fatalf("config response = %#v, %v", got, err)
	}

	want := ConfigUpdate{
		UUID:            state.UUID,
		OutboundEnabled: map[string]bool{"vless-reality": false, "tuic": true},
	}
	body, _ := json.Marshal(want)
	response, err = http.Post(base+"config", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent || saved.UUID != want.UUID || saved.OutboundEnabled["tuic"] != true {
		t.Fatalf("save = %s %#v", response.Status, saved)
	}

	response, err = http.Post(base+"close", "application/json", bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	select {
	case <-closed:
	default:
		t.Fatal("close callback was not called")
	}
}

func TestClientReadsAPIAddressAndSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "singbox.json")
	if err := os.WriteFile(path, []byte(`{"services":[{"type":"api","listen":"127.0.0.1","listen_port":40123,"secret":"token"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(path)
	if err != nil {
		t.Fatal(err)
	}
	if client.address != "127.0.0.1:40123" || client.secret != "token" {
		t.Fatalf("client = %#v", client)
	}
}

func TestWireMessagesRoundTripThroughProtobufAdapter(t *testing.T) {
	want := &groupsMessage{Groups: []*groupMessage{{
		Tag: "proxy-selector", Type: "Selector", Selectable: true, Selected: "tuic",
		Items: []*groupItemMessage{{Tag: "tuic", Type: "TUIC", URLTestDelay: 75}},
	}}}
	data, err := proto.Marshal(protoadapt.MessageV2Of(want))
	if err != nil {
		t.Fatal(err)
	}
	var got groupsMessage
	if err := proto.Unmarshal(data, protoadapt.MessageV2Of(&got)); err != nil {
		t.Fatal(err)
	}
	if len(got.Groups) != 1 || got.Groups[0].Selected != "tuic" || got.Groups[0].Items[0].URLTestDelay != 75 {
		t.Fatalf("round trip = %#v", got.Groups)
	}
}
