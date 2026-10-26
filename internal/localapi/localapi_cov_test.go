package localapi

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"jorbiquevpn/internal/jsonorder"

	"google.golang.org/protobuf/protoadapt"
)

// serve runs handler against one recorded request.
func serve(handler http.Handler, method, target, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func writeAPIConfig(t *testing.T, address string) string {
	t.Helper()
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	portNumber, _ := strconv.Atoi(port)
	data, _ := json.Marshal(map[string]any{"services": []any{map[string]any{"type": "api", "listen": host, "listen_port": portNumber}}})
	path := filepath.Join(t.TempDir(), "singbox.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTrafficFailsWithoutAReachableAPI(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	stoppedAddress := listener.Addr().String()
	listener.Close()

	_, runningPath := startFakeStartedService(t)
	running, err := NewClient(runningPath)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name       string
		configPath string
	}{
		// A stopped tunnel is routine while the main window polls.
		{"stopped tunnel", writeAPIConfig(t, stoppedAddress)},
		// The config has no secret, so the running API rejects the call.
		{"unauthenticated", writeAPIConfig(t, running.address)},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, err := NewClient(test.configPath)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Traffic(context.Background()); err == nil {
				t.Fatal("Traffic succeeded, want an error")
			}
		})
	}
}

func TestFormActionHandler(t *testing.T) {
	var received string
	handler := formActionHandler("choose-file", func(body json.RawMessage) (any, error) {
		received = string(body)
		return map[string]string{"path": "/tmp/rules.txt"}, nil
	})
	if response := serve(handler, http.MethodGet, "/api/choose-file", ""); response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d, want 405", response.Code)
	}
	if response := serve(handler, http.MethodPost, "/api/choose-file", strings.Repeat("x", 64<<10+1)); response.Code != http.StatusBadRequest {
		t.Fatalf("oversized status = %d, want 400", response.Code)
	}
	if received != "" {
		t.Fatalf("handler ran for a rejected request with %q", received)
	}
	response := serve(handler, http.MethodPost, "/api/choose-file", `{"kind":"rules"}`)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response = %d %v", response.Code, response.Header())
	}
	if strings.TrimSpace(response.Body.String()) != `{"path":"/tmp/rules.txt"}` || received != `{"kind":"rules"}` {
		t.Fatalf("body = %q, handler received %q", response.Body, received)
	}
}

func TestFormStateHandlerRejectsBadRequests(t *testing.T) {
	state := func() any { return map[string]int{"version": 1} }
	saveCalls := 0
	save := func(json.RawMessage) error { saveCalls++; return nil }
	for _, test := range []struct {
		name, method, body string
		save               func(json.RawMessage) error
		wantStatus         int
	}{
		{"wrong method", http.MethodPut, "{}", save, http.StatusMethodNotAllowed},
		{"oversized", http.MethodPost, strings.Repeat("x", 64<<10+1), save, http.StatusBadRequest},
		{"no save", http.MethodPost, "{}", nil, http.StatusNotImplemented},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := serve(formStateHandler(state, test.save), test.method, "/api/settings", test.body)
			if response.Code != test.wantStatus || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("response = %d %v, want %d with no-store", response.Code, response.Header(), test.wantStatus)
			}
		})
	}
	if saveCalls != 0 {
		t.Fatalf("save called %d times for rejected requests", saveCalls)
	}
}

func TestFormServerCloseNeedsPostAndToleratesNoCallback(t *testing.T) {
	server, err := StartFormServer("", "settings", fstest.MapFS{}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	closeURL := strings.TrimSuffix(server.URL, "?") + "api/close"
	for method, want := range map[string]int{http.MethodGet: http.StatusMethodNotAllowed, http.MethodPost: http.StatusNoContent} {
		response := serve(server.server.Handler, method, closeURL, "")
		if response.Code != want {
			t.Fatalf("%s close status = %d, want %d", method, response.Code, want)
		}
	}
}

func TestConfigServerRejectsMalformedUpdateAndSavesWithoutCallback(t *testing.T) {
	server, err := StartConfigServer("", ConfigState{}, fstest.MapFS{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	configURL := strings.TrimSuffix(server.URL, "?") + "api/config"
	response := serve(server.server.Handler, http.MethodPost, configURL, `{"uuid":1}`)
	if response.Code != http.StatusBadGateway || strings.TrimSpace(response.Body.String()) != invalidRequest {
		t.Fatalf("malformed update = %d %q, want 502 %q", response.Code, response.Body, invalidRequest)
	}
	response = serve(server.server.Handler, http.MethodPost, configURL, `{"uuid":"7998568d-7ae4-4f65-a4cb-b20e78af6a87"}`)
	if response.Code != http.StatusNoContent {
		t.Fatalf("update without save callback = %d, want 204", response.Code)
	}
}

func TestWireMessagesResetToZero(t *testing.T) {
	for _, message := range []protoadapt.MessageV1{
		&statusRequest{Interval: 1},
		&statusMessage{UplinkTotal: 1, DownlinkTotal: 2},
		&groupsMessage{Groups: []*groupMessage{{Tag: "proxy"}}},
		&groupMessage{Tag: "proxy", Selected: "tuic"},
		&groupItemMessage{Tag: "tuic", URLTestDelay: 75},
		&selectOutboundMessage{GroupTag: "proxy", OutboundTag: "tuic"},
	} {
		message.ProtoMessage()
		if got := message.String(); got != "" {
			t.Errorf("%T.String() = %q, want empty", message, got)
		}
		message.Reset()
		if !reflect.ValueOf(message).Elem().IsZero() {
			t.Errorf("%T.Reset() left %#v", message, message)
		}
	}
}

func TestConfigureAPIKeepsOneAPIServiceAndForeignHTTPClients(t *testing.T) {
	first := jsonorder.FromMap(map[string]any{"type": "api", "tag": "first"})
	other := jsonorder.FromMap(map[string]any{"type": "ssm-api", "tag": "ssm"})
	direct := jsonorder.FromMap(map[string]any{"tag": "direct-http"})
	root := jsonorder.FromMap(map[string]any{
		"services":     []any{"opaque", first, jsonorder.FromMap(map[string]any{"type": "api", "tag": "duplicate"}), other},
		"http_clients": []any{jsonorder.FromMap(map[string]any{"tag": "dashboard-http"}), direct},
	})
	if err := ConfigureAPI(root, "  fixed-value  "); err != nil {
		t.Fatal(err)
	}
	services, _ := root.Get("services")
	if want := []any{"opaque", first, other}; !reflect.DeepEqual(services, want) {
		t.Fatalf("services = %v, want the first API service and non-API entries", services)
	}
	if got := APISecret(root); got != "fixed-value" || first.GetString("listen") != "127.0.0.1" {
		t.Fatalf("API service = %v, secret %q", first, got)
	}
	clients, _ := root.Get("http_clients")
	if want := []any{direct}; !reflect.DeepEqual(clients, want) {
		t.Fatalf("http_clients = %v, want only direct-http", clients)
	}
}

func TestAPISecretWithoutAPIService(t *testing.T) {
	for name, root := range map[string]*jsonorder.Map{
		"nil root":    nil,
		"no services": jsonorder.New(),
		"no API":      jsonorder.FromMap(map[string]any{"services": []any{jsonorder.FromMap(map[string]any{"type": "dns"})}}),
	} {
		if got := APISecret(root); got != "" {
			t.Errorf("%s: APISecret = %q, want empty", name, got)
		}
	}
}
