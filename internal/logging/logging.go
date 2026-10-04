package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	LevelWarn = "WARNING"
)

var (
	mu          sync.Mutex
	logFile     string
	initialized bool
)

// Initialize sets up logging to file. Safe to call multiple times; same path is a no-op so the file is not truncated.
func Initialize(logFileName, projectRoot string) {
	mu.Lock()
	defer mu.Unlock()
	if projectRoot == "" {
		return
	}
	logsDir := filepath.Join(projectRoot, "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return
	}
	newLogFile := filepath.Join(logsDir, logFileName)
	if initialized && logFile == newLogFile {
		return
	}
	logFile = newLogFile
	ts := time.Now().Format("2006-01-02 15:04:05")
	header := fmt.Sprintf("=== Script log started at %s ===\r\n", ts)
	// New or empty file: write header. Existing non-empty file: append session marker (do not truncate history).
	if st, err := os.Stat(logFile); err == nil && st.Size() > 0 {
		f, err := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0644)
		if err == nil {
			_, _ = fmt.Fprintf(f, "\r\n=== Log session at %s ===\r\n", ts)
			_ = f.Close()
		}
	} else {
		_ = os.WriteFile(logFile, []byte(header), 0644)
	}
	initialized = true
}

// Write logs a message with level. Silently skips if not initialized or file cannot be opened.
func Write(message, level string) {
	mu.Lock()
	logPath := logFile
	ok := initialized && logPath != ""
	mu.Unlock()
	if !ok {
		return
	}
	ts := time.Now().Format("2006-01-02 15:04:05")
	line := fmt.Sprintf("[%s] [%s] %s\r\n", ts, level, message)
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	_, _ = f.WriteString(line)
	_ = f.Close()
}

func ClearAll(projectRoot string) {
	logsDir := filepath.Join(projectRoot, "logs")
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			_ = os.Remove(filepath.Join(logsDir, e.Name()))
		}
	}
}
