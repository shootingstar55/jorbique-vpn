package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"jorbiquevpn/internal/logging"
	"jorbiquevpn/internal/macos/ansiutil"
	"jorbiquevpn/internal/macos/core"
)

// The Logs window's file handling, apart from the window so the Linux CI tests it.

// userLogPrefix marks the menu bar's own logs in the list. They live in the
// user's Library rather than root's, and one of them is called app.log too.
const userLogPrefix = "menu bar/"

// logFileNames lists the logs the window can show: root's first — the daemon,
// the commands it runs, sing-box — then the menu bar's own.
func logFileNames(paths *core.Paths) []string {
	names := logNamesIn(paths.SystemLogDir, "")
	if paths.LogDir != paths.SystemLogDir {
		names = append(names, logNamesIn(paths.LogDir, userLogPrefix)...)
	}
	return names
}

func logNamesIn(dir, prefix string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
			names = append(names, prefix+e.Name())
		}
	}
	return names
}

// logPath resolves a name from the list back to its file. Only a plain *.log
// name in one of the two log directories: the name comes from a web page.
func logPath(paths *core.Paths, name string) (string, error) {
	dir := paths.SystemLogDir
	if rest, ok := strings.CutPrefix(name, userLogPrefix); ok {
		dir, name = paths.LogDir, rest
	}
	if name != filepath.Base(name) || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".log") {
		return "", fmt.Errorf("invalid log file name")
	}
	return filepath.Join(dir, name), nil
}

// readLog is the newest part of a log, without the colour codes sing-box
// writes when its output is a launchd file rather than a terminal.
func readLog(paths *core.Paths, name string) (string, error) {
	path, err := logPath(paths, name)
	if err != nil {
		return "", err
	}
	text, err := logging.ReadTail(path, logging.ViewerMaxBytes)
	if err != nil {
		return "", err
	}
	return ansiutil.Strip(text), nil
}
