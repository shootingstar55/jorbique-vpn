package localapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// serve runs handler against one recorded request.
func serve(handler http.Handler, method, target, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
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
