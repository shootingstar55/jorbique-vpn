package localapi

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"testing"
	"testing/fstest"
)

func TestLocalServerServesOnlyTokenScopedAssets(t *testing.T) {
	assets := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("jorbique-panel")}}
	server, err := StartFormServer("main=1", "main", fs.FS(assets), struct{}{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/api/main"} {
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
	if response.StatusCode != http.StatusOK || string(body) != "jorbique-panel" {
		t.Fatalf("panel response = %s %q", response.Status, body)
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
