package dashboard

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
)

const (
	methodNotAllowed = "method not allowed"
	invalidRequest   = "invalid request"
)

type LocalServer struct {
	server *http.Server
	URL    string
}

type ConfigState struct {
	UUID               string          `json:"uuid"`
	AvailableOutbounds []string        `json:"availableOutbounds"`
	OutboundEnabled    map[string]bool `json:"outboundEnabled"`
	NaiveMode          bool            `json:"naiveMode"`
}

type ConfigUpdate struct {
	UUID            string          `json:"uuid"`
	OutboundEnabled map[string]bool `json:"outboundEnabled"`
	// NaiveMode is nil from a page that does not know the mode, which keeps
	// what is stored.
	NaiveMode *bool `json:"naiveMode,omitempty"`
}

// StartLocalServer serves embedded UI and a token-scoped loopback bridge for
// WKWebView. No CORS: dashboard and bridge share one private local origin.
func StartLocalServer(query, configPath string, assets fs.FS, memoryFallback func() uint64) (*LocalServer, error) {
	prefix, listener, mux, err := newLocalMux()
	if err != nil {
		return nil, err
	}

	mux.HandleFunc(prefix+"api/snapshot", snapshotHandler(configPath, memoryFallback))

	post := func(path string, action func(context.Context, *Client, []string) error) {
		mux.HandleFunc(prefix+"api/"+path, postHandler(configPath, path, action))
	}
	post("select", func(ctx context.Context, api *Client, values []string) error {
		if len(values) != 2 {
			return fmt.Errorf("group and outbound are required")
		}
		return api.SelectOutbound(ctx, values[0], values[1])
	})
	post("url-test", func(ctx context.Context, api *Client, values []string) error {
		if len(values) != 1 {
			return fmt.Errorf("outbound is required")
		}
		return api.URLTest(ctx, values[0])
	})
	return serveLocal(listener, mux, prefix, assets, query), nil
}

// StartFormServer serves one of the shared Vue forms to WKWebView: GET
// /api/<path> hands the window its initial values, POST /api/<path> saves them,
// and POST /api/close closes the window from a Cancel button.
//
// Every macOS panel is one of these — the config form, the route rules, the
// main window — so they differ by their state type and their save function
// rather than by a server each.
func StartFormServer(query, path string, assets fs.FS, state any, save func(json.RawMessage) error, closeWindow func()) (*LocalServer, error) {
	return startFormServer(query, path, assets, func() any { return state }, save, nil, closeWindow)
}

// StartFormServerWithActions is StartFormServer for a page that also needs to
// ask the app for something mid-edit and get an answer back — the route rules
// page opening the native file chooser, which a web page cannot do itself. Each
// action is POST /api/<name> and answers with JSON.
// StartFormServerFunc is StartFormServer for a window whose values change while
// it is open — the main window polls its status — so the state is read per
// request rather than captured once.
func StartFormServerFunc(query, path string, assets fs.FS, state func() any, save func(json.RawMessage) error, actions map[string]func(json.RawMessage) (any, error), closeWindow func()) (*LocalServer, error) {
	return startFormServer(query, path, assets, state, save, actions, closeWindow)
}

func StartFormServerWithActions(query, path string, assets fs.FS, state any, save func(json.RawMessage) error, actions map[string]func(json.RawMessage) (any, error), closeWindow func()) (*LocalServer, error) {
	return startFormServer(query, path, assets, func() any { return state }, save, actions, closeWindow)
}

func startFormServer(query, path string, assets fs.FS, state func() any, save func(json.RawMessage) error, actions map[string]func(json.RawMessage) (any, error), closeWindow func()) (*LocalServer, error) {
	prefix, listener, mux, err := newLocalMux()
	if err != nil {
		return nil, err
	}
	for name, action := range actions {
		mux.HandleFunc(prefix+"api/"+name, formActionHandler(name, action))
	}
	mux.HandleFunc(prefix+"api/"+path, formStateHandler(state, save))
	mux.HandleFunc(prefix+"api/close", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			http.Error(writer, methodNotAllowed, http.StatusMethodNotAllowed)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
		if closeWindow != nil {
			closeWindow()
		}
	})
	return serveLocal(listener, mux, prefix, assets, query), nil
}

// StartConfigServer serves the shared Vue VPN configuration form to WKWebView.
func StartConfigServer(query string, state ConfigState, assets fs.FS, save func(ConfigUpdate) error, closeWindow func()) (*LocalServer, error) {
	return StartFormServer(query, "config", assets, state, func(body json.RawMessage) error {
		var update ConfigUpdate
		if err := json.Unmarshal(body, &update); err != nil {
			return errors.New(invalidRequest)
		}
		if save == nil {
			return nil
		}
		return save(update)
	}, closeWindow)
}

func newLocalMux() (string, net.Listener, *http.ServeMux, error) {
	tokenBytes := make([]byte, 18)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", nil, nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, nil, err
	}
	return "/" + base64.RawURLEncoding.EncodeToString(tokenBytes) + "/", listener, http.NewServeMux(), nil
}

func serveLocal(listener net.Listener, mux *http.ServeMux, prefix string, assets fs.FS, query string) *LocalServer {
	mux.Handle(prefix, http.StripPrefix(prefix, http.FileServer(http.FS(assets))))
	server := &http.Server{Handler: mux}
	result := &LocalServer{server: server, URL: "http://" + listener.Addr().String() + prefix + "?" + query}
	go func() { _ = server.Serve(listener) }()
	return result
}

func (s *LocalServer) Close() {
	if s != nil && s.server != nil {
		_ = s.server.Close()
	}
}

func respondError(writer http.ResponseWriter, err error) {
	http.Error(writer, err.Error(), http.StatusBadGateway)
}

func snapshotHandler(configPath string, memoryFallback func() uint64) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			http.Error(writer, methodNotAllowed, http.StatusMethodNotAllowed)
			return
		}
		api, err := NewClient(configPath)
		if err != nil {
			respondError(writer, err)
			return
		}
		result, err := api.Snapshot(request.Context())
		if err != nil {
			respondError(writer, err)
			return
		}
		if result.Status.Memory == 0 && memoryFallback != nil {
			result.Status.Memory = memoryFallback()
		}
		writeJSON(writer, result)
	}
}

func postHandler(configPath, path string, action func(context.Context, *Client, []string) error) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			http.Error(writer, methodNotAllowed, http.StatusMethodNotAllowed)
			return
		}
		request.Body = http.MaxBytesReader(writer, request.Body, 8<<10)
		var values []string
		if err := json.NewDecoder(request.Body).Decode(&values); err != nil {
			http.Error(writer, invalidRequest, http.StatusBadRequest)
			return
		}
		api, err := NewClient(configPath)
		if err == nil {
			err = action(request.Context(), api, values)
		}
		if err != nil {
			respondError(writer, err)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}
}

func formActionHandler(name string, handler func(json.RawMessage) (any, error)) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			http.Error(writer, methodNotAllowed, http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, 64<<10))
		if err != nil {
			http.Error(writer, invalidRequest, http.StatusBadRequest)
			return
		}
		result, err := handler(body)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(writer, result)
	}
}

func formStateHandler(state func() any, save func(json.RawMessage) error) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		if request.Method == http.MethodGet {
			writeJSON(writer, state())
			return
		}
		if request.Method != http.MethodPost {
			http.Error(writer, methodNotAllowed, http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, 64<<10))
		if err != nil {
			http.Error(writer, invalidRequest, http.StatusBadRequest)
			return
		}
		if save == nil {
			http.Error(writer, "save unavailable", http.StatusNotImplemented)
			return
		}
		if err := save(body); err != nil {
			// Save callbacks report failures at their source; CLI children also
			// report before returning their exit status and local output.
			http.Error(writer, err.Error(), http.StatusBadGateway)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}
}

func writeJSON(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(writer).Encode(value)
}
