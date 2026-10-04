//go:build windows

package utils

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unsafe"

	"golang.org/x/sys/windows"
)

type ActiveProcess struct {
	PID         int    `json:"pid"`
	ProcessName string `json:"process_name"` // without extension
	ExeName     string `json:"exe_name"`     // with .exe when available
	Path        string `json:"path,omitempty"`
	WindowTitle string `json:"window_title,omitempty"`
	Icon        string `json:"icon,omitempty"` // data:image/png;base64,...
}

// ListActiveApps returns processes that look like "Apps" in Task Manager:
// those with a main window handle. This is best-effort and may omit background processes.
func ListActiveApps() ([]ActiveProcess, error) {
	procs, err := windowedProcesses()
	if err != nil {
		return nil, err
	}

	selfPID := os.Getpid()
	selfBase := ""
	if exe, err := os.Executable(); err == nil {
		selfBase = strings.TrimSuffix(strings.ToLower(filepath.Base(exe)), filepath.Ext(filepath.Base(exe)))
	}

	// Filter out noisy/system entries and ourselves.
	hidden := map[string]bool{
		"applicationframehost": true,
		"explorer":             true,
		"svchost":              true,
		"systemsettings":       true,
		"textinputhost":        true,
	}

	filtered := make([]ActiveProcess, 0, len(procs))
	for _, p := range procs {
		nameKey := strings.ToLower(strings.TrimSpace(p.ProcessName))
		if nameKey == "" {
			continue
		}
		if p.PID == selfPID || (selfBase != "" && nameKey == selfBase) {
			continue
		}
		if hidden[nameKey] {
			continue
		}
		// Hide NVIDIA-related utility windows (overlay/app/container helpers etc.)
		if strings.HasPrefix(nameKey, "nvidia") || strings.HasPrefix(nameKey, "nvcontainer") || nameKey == "nvcontainer" || nameKey == "nvidiashare" || nameKey == "nvidiaoverlay" {
			continue
		}
		p.WindowTitle = cleanWindowTitle(p.WindowTitle)
		filtered = append(filtered, p)
	}
	return dedupeByName(filtered), nil
}

// dedupeByName deduplicates by ProcessName (keep lowest PID) and sorts for stable UI.
func dedupeByName(procs []ActiveProcess) []ActiveProcess {
	byName := map[string]ActiveProcess{}
	for _, p := range procs {
		key := strings.ToLower(p.ProcessName)
		if cur, ok := byName[key]; !ok || (p.PID > 0 && (cur.PID == 0 || p.PID < cur.PID)) {
			byName[key] = p
		}
	}
	procs = procs[:0]
	for _, p := range byName {
		procs = append(procs, p)
	}
	sort.Slice(procs, func(i, j int) bool {
		return strings.ToLower(procs[i].ProcessName) < strings.ToLower(procs[j].ProcessName)
	})
	return procs
}

// cleanWindowTitle trims a window title and hides it when it looks garbled.
func cleanWindowTitle(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// If Windows gives us mojibake / replacement characters, hide the title.
	if strings.ContainsRune(s, '\uFFFD') {
		return ""
	}
	bad := 0
	total := 0
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r' {
			bad++
		}
		total++
		if total >= 80 {
			break
		}
	}
	if total > 0 && bad*5 > total { // >20% control chars
		return ""
	}
	return s
}

var (
	procGetWindow         = moduser32.NewProc("GetWindow")
	procGetWindowTextW    = moduser32.NewProc("GetWindowTextW")
	procGetWindowTextLenW = moduser32.NewProc("GetWindowTextLengthW")
)

const gwOwner = 4 // GW_OWNER

// windowedProcesses lists one entry per process that owns a visible top-level window — the same
// set PowerShell reported as "MainWindowHandle -ne 0", enumerated through user32 instead of
// launching a shell and a .NET runtime to ask.
func windowedProcesses() ([]ActiveProcess, error) {
	var procs []ActiveProcess
	seen := map[uint32]bool{}
	callback := windows.NewCallback(func(hwnd windows.HWND, _ uintptr) uintptr {
		if !windows.IsWindowVisible(hwnd) || ownerWindow(hwnd) != 0 {
			return 1 // keep enumerating
		}
		var pid uint32
		if _, err := windows.GetWindowThreadProcessId(hwnd, &pid); err != nil || pid == 0 || seen[pid] {
			return 1
		}
		seen[pid] = true
		p := ActiveProcess{PID: int(pid), WindowTitle: windowText(hwnd)}
		if path, ok := processImagePath(pid); ok {
			p.Path = path
			base := filepath.Base(path)
			p.ProcessName = strings.TrimSuffix(base, filepath.Ext(base))
			p.ExeName = p.ProcessName + ".exe"
			p.Icon = iconDataURL(path)
		}
		if p.ProcessName != "" {
			procs = append(procs, p)
		}
		return 1
	})
	if err := windows.EnumWindows(callback, nil); err != nil {
		return nil, err
	}
	return procs, nil
}

func ownerWindow(hwnd windows.HWND) uintptr {
	owner, _, _ := procGetWindow.Call(uintptr(hwnd), gwOwner)
	return owner
}

func windowText(hwnd windows.HWND) string {
	length, _, _ := procGetWindowTextLenW.Call(uintptr(hwnd))
	if length == 0 {
		return ""
	}
	buf := make([]uint16, length+1)
	copied, _, _ := procGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:copied])
}

// processImagePath returns the full path of a running process. Reading it needs no more than
// query-limited access, which an elevated client has for everything with a window on screen.
func processImagePath(pid uint32) (string, bool) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return "", false
	}
	return windows.UTF16ToString(buf[:size]), true
}
