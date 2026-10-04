package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetDefaultConfig(t *testing.T) {
	cfg := GetDefaultConfig()
	if cfg.GatewayIP == "" {
		t.Error("GatewayIP should not be empty")
	}
	if cfg.MaxStartupChecks <= 0 {
		t.Error("MaxStartupChecks should be positive")
	}
}

func TestInitialize(t *testing.T) {
	wd, _ := os.Getwd()
	// Try wd and parent (when running from internal/config)
	projectRoot := wd
	for i := 0; i < 3; i++ {
		if _, err := os.Stat(filepath.Join(projectRoot, "singbox_cfg.yml")); err == nil {
			break
		}
		projectRoot = filepath.Dir(projectRoot)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, "singbox_cfg.yml")); os.IsNotExist(err) {
		t.Skip("singbox_cfg.yml not found, skipping")
	}
	result, err := Initialize("singbox_cfg.yml", projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	if result.GatewayIP == "" {
		t.Error("GatewayIP should be set")
	}
}
