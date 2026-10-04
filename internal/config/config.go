package config

import (
	"fmt"
	"os"
	"path/filepath"

	"jorbiquevpn/internal/assets"
	"jorbiquevpn/internal/constants"

	"gopkg.in/yaml.v3"
)

type Config struct {
	GatewayIP         string
	MaxStartupChecks  int
	StartupCheckDelay int
}

type YamlConfig struct {
	Tun struct {
		GatewayIP string `yaml:"gateway_ip"`
	} `yaml:"tun"`
	Startup struct {
		MaxChecks  int `yaml:"max_checks"`
		CheckDelay int `yaml:"check_delay"`
	} `yaml:"startup"`
}

func GetDefaultConfig() Config {
	return Config{
		GatewayIP:         constants.SingboxGatewayIP,
		MaxStartupChecks:  constants.MaxStartupChecks,
		StartupCheckDelay: constants.StartupCheckDelaySec,
	}
}

// Initialize loads config: 1) external file if path given, 2) embedded singbox_cfg.yml, 3) defaults.
func Initialize(configPath, projectRoot string) (Config, error) {
	defaults := GetDefaultConfig()

	var data []byte
	var err error

	if configPath != "" && projectRoot != "" {
		fullPath := configPath
		if !filepath.IsAbs(configPath) {
			fullPath = filepath.Join(projectRoot, configPath)
		}
		fullPathNorm, _ := filepath.Abs(fullPath)
		projectRootNorm, _ := filepath.Abs(projectRoot)
		if len(projectRootNorm) > 0 && len(fullPathNorm) >= len(projectRootNorm) && fullPathNorm[:len(projectRootNorm)] == projectRootNorm {
			data, err = os.ReadFile(fullPath)
			if err == nil {
				goto parse
			}
		}
	}

	data, err = assets.FS.ReadFile("singbox_cfg.yml")
	if err != nil {
		return defaults, nil
	}

parse:
	var yc YamlConfig
	if err := yaml.Unmarshal(data, &yc); err != nil {
		return defaults, fmt.Errorf("singbox_cfg.yml: %w", err)
	}

	cfg := Config{
		GatewayIP:         defaults.GatewayIP,
		MaxStartupChecks:  defaults.MaxStartupChecks,
		StartupCheckDelay: defaults.StartupCheckDelay,
	}
	if yc.Tun.GatewayIP != "" {
		cfg.GatewayIP = yc.Tun.GatewayIP
	}
	if yc.Startup.MaxChecks > 0 {
		cfg.MaxStartupChecks = yc.Startup.MaxChecks
	}
	if yc.Startup.CheckDelay > 0 {
		cfg.StartupCheckDelay = yc.Startup.CheckDelay
	}
	return cfg, nil
}
