//go:build !darwin

package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"jorbiquevpn/internal/constants"
	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/sysexec"
)

// RunStatusBarApp is used when GOOS != darwin (e.g. a Linux or Windows build ran on macOS).
// The real UI is in ui_darwin.go and only compiles for darwin. Finder launches with no TTY:
// stderr is invisible, so we also show an osascript dialog.
func RunStatusBarApp() {
	p := core.NewPaths()
	_ = os.MkdirAll(p.LogDir, 0o755)
	line := fmt.Sprintf("[%s] Menu bar unavailable: binary not built for macOS (wrong GOOS). "+
		"Use the darwin build from CI or build locally with GOOS=darwin. CLI: jorbique-vpn help / start …\n",
		time.Now().Format(time.RFC3339))
	logPath := filepath.Join(p.LogDir, "app.log")
	if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		_, _ = f.WriteString(line)
		_ = f.Close()
	} else if home, e := os.UserHomeDir(); e == nil && home != "" {
		// Default install under /usr/local may leave logs/ root-owned after sudo bootstrap; user cannot append.
		alt := filepath.Join(home, "Library", "Logs", "JorbiqueVPN", "app.log")
		_ = os.MkdirAll(filepath.Dir(alt), 0o755)
		if f2, e2 := os.OpenFile(alt, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); e2 == nil {
			_, _ = f2.WriteString(line)
			_, _ = f2.WriteString(fmt.Sprintf("[%s] (primary log %s not writable: %v)\n", time.Now().Format(time.RFC3339), logPath, err))
			_ = f2.Close()
		}
	}
	_, _ = fmt.Fprint(os.Stderr, line)

	// Double-click from Finder: no window for stderr — explain via GUI.
	showNoCGODialog()

	os.Exit(core.ExitGeneral)
}

func showNoCGODialog() {
	msg := "Menu bar is unavailable: this binary was not built for macOS (wrong GOOS). " +
		"Use the darwin build. Use Terminal: " +
		"jorbique-vpn help. Details: ~/Library/Logs/JorbiqueVPN/app.log"
	// AppleScript: escape backslashes and double quotes for -e "…"
	esc := strings.ReplaceAll(msg, `\`, `\\`)
	esc = strings.ReplaceAll(esc, `"`, `\"`)
	script := `display dialog "` + esc + `" buttons {"OK"} default button "OK" with title "` + constants.AppDisplayName + `"`
	_ = sysexec.Command("/usr/bin/osascript", "-e", script).Run()
}

// ForwardLaunchLink has no AppKit to take a link from off darwin.
func ForwardLaunchLink() {}
