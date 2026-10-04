package paths

import (
	"os"
	"path/filepath"

	"jorbiquevpn/internal/constants"
)

// DataDir returns %APPDATA%/JorbiqueVPN on Windows.
func DataDir() string {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		home, _ := os.UserHomeDir()
		appData = filepath.Join(home, "AppData", "Roaming")
	}
	return filepath.Join(appData, constants.AppDataDirName)
}

// RuntimeConfigPath returns runtime_config.json under the app data directory (not %TEMP%).
func RuntimeConfigPath() string {
	data := DataDir()
	_ = os.MkdirAll(data, 0755)
	return filepath.Join(data, "runtime_config.json")
}

// EnsureDataDirs creates logs, cache (including tun subdirs), and data directories
func EnsureDataDirs() error {
	data := DataDir()
	dirs := []string{
		data,
		filepath.Join(data, "logs"),
		filepath.Join(data, "cache"),
		filepath.Join(data, "cache", "tun"),
		filepath.Join(data, "webview"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	return nil
}
