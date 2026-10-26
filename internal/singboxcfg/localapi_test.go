package singboxcfg

import (
	"reflect"
	"testing"

	"jorbiquevpn/internal/jsonorder"
)

func TestRemoveLocalAPIKeepsOtherServicesAndHTTPClients(t *testing.T) {
	other := jsonorder.FromMap(map[string]any{"type": "ssm-api", "tag": "ssm"})
	direct := jsonorder.FromMap(map[string]any{"tag": "direct-http"})
	root := jsonorder.FromMap(map[string]any{
		"services": []any{
			"opaque",
			jsonorder.FromMap(map[string]any{"type": "api", "tag": "local-api", "listen": "127.0.0.1", "secret": "x"}),
			jsonorder.FromMap(map[string]any{"type": "api", "tag": "duplicate"}),
			other,
		},
		"http_clients": []any{jsonorder.FromMap(map[string]any{"tag": "dashboard-http"}), direct},
	})
	RemoveLocalAPI(root)
	services, _ := root.Get("services")
	if want := []any{"opaque", other}; !reflect.DeepEqual(services, want) {
		t.Fatalf("services = %v, want only the non-API entries", services)
	}
	clients, _ := root.Get("http_clients")
	if want := []any{direct}; !reflect.DeepEqual(clients, want) {
		t.Fatalf("http_clients = %v, want only direct-http", clients)
	}
}

func TestRemoveLocalAPIDropsEmptiedLists(t *testing.T) {
	root := jsonorder.FromMap(map[string]any{
		"services":     []any{jsonorder.FromMap(map[string]any{"type": "api"})},
		"http_clients": []any{jsonorder.FromMap(map[string]any{"tag": "dashboard-http"})},
	})
	RemoveLocalAPI(root)
	if root.Has("services") || root.Has("http_clients") {
		t.Fatalf("emptied lists remain: %v", root)
	}
	RemoveLocalAPI(jsonorder.New()) // nothing to remove is fine too
}
