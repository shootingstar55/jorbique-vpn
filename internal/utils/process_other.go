//go:build !windows

package utils

// ActiveProcess is a minimal cross-platform shape used by the UI.
type ActiveProcess struct {
	PID         int    `json:"pid"`
	ProcessName string `json:"process_name"`
	ExeName     string `json:"exe_name"`
	Path        string `json:"path,omitempty"`
	WindowTitle string `json:"window_title,omitempty"`
	Icon        string `json:"icon,omitempty"`
}

// ListActiveApps is Windows-only for now.
func ListActiveApps() ([]ActiveProcess, error) {
	return []ActiveProcess{}, nil
}
