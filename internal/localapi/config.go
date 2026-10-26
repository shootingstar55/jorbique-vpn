package localapi

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"jorbiquevpn/internal/jsonorder"
)

// ConfigureAPI installs one loopback-only StartedService endpoint. JorbiqueVPN
// renders its own UI, so sing-box's downloadable dashboard is removed.
func ConfigureAPI(root *jsonorder.Map, secret string) error {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		secretBytes := make([]byte, 32)
		if _, err := rand.Read(secretBytes); err != nil {
			return fmt.Errorf("generate sing-box API secret: %w", err)
		}
		secret = base64.RawURLEncoding.EncodeToString(secretBytes)
	}

	var services []any
	if raw, ok := root.Get("services"); ok {
		services, _ = raw.([]any)
	}
	var api *jsonorder.Map
	keptServices := services[:0]
	for _, item := range services {
		service, ok := item.(*jsonorder.Map)
		if !ok || service.GetString("type") != "api" {
			keptServices = append(keptServices, item)
			continue
		}
		if api == nil {
			api = service
			keptServices = append(keptServices, item)
		}
	}
	services = keptServices
	if api == nil {
		api = jsonorder.New()
		api.Set("type", "api")
		api.Set("tag", "local-api")
		services = append(services, api)
	}
	api.Set("listen", "127.0.0.1")
	api.Set("listen_port", Port)
	api.Set("secret", secret)
	api.Delete("dashboard")
	root.Set("services", services)

	removeDashboardHTTPClient(root)
	return nil
}

// APISecret returns the first API service secret from an existing local config.
func APISecret(root *jsonorder.Map) string {
	if root == nil {
		return ""
	}
	raw, ok := root.Get("services")
	if !ok {
		return ""
	}
	services, _ := raw.([]any)
	for _, item := range services {
		service, ok := item.(*jsonorder.Map)
		if ok && service.GetString("type") == "api" {
			return strings.TrimSpace(service.GetString("secret"))
		}
	}
	return ""
}

func removeDashboardHTTPClient(root *jsonorder.Map) {
	if raw, ok := root.Get("http_clients"); ok {
		clients, _ := raw.([]any)
		kept := clients[:0]
		for _, item := range clients {
			client, ok := item.(*jsonorder.Map)
			if ok && client.GetString("tag") == "dashboard-http" {
				continue
			}
			kept = append(kept, item)
		}
		if len(kept) == 0 {
			root.Delete("http_clients")
		} else {
			root.Set("http_clients", kept)
		}
	}
}
