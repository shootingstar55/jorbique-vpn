package logging

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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

// ClearAll empties every log under projectRoot/logs. It truncates rather than
// removes: sing-box keeps its log open, and a removed file it goes on writing
// to is gone from the list until the next connect.
func ClearAll(projectRoot string) {
	logsDir := filepath.Join(projectRoot, "logs")
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.Type().IsRegular() {
			_ = os.Truncate(filepath.Join(logsDir, e.Name()), 0)
		}
	}
}

// ViewerMaxBytes is how much of a log the Logs window shows: the newest part.
// sing-box at debug grows a log by megabytes, and a viewer that loads all of it
// freezes the window it is drawn in.
const ViewerMaxBytes = 2 << 20

// ReadTail returns the last max bytes of a file, starting on a whole line, with
// a note on top when the start was cut off. A log that does not exist yet, as
// after Clear until the next line is written, reads as empty.
func ReadTail(path string, max int64) (string, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	if st.Size() <= max {
		data, err := io.ReadAll(f)
		return string(data), err
	}
	data := make([]byte, max)
	if _, err := f.ReadAt(data, st.Size()-max); err != nil && err != io.EOF {
		return "", err
	}
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		data = data[i+1:]
	}
	note := fmt.Sprintf("… последние %d КБ из %d КБ\n", len(data)>>10, st.Size()>>10)
	return note + string(data), nil
}

// MaxLogBytes is the size at which a log starts over from empty.
const MaxLogBytes = 6 << 20

// LogCapInterval is how often CapLogsEvery looks. sing-box at debug writes a
// few hundred kilobytes in that time, which is how far past the cap a log can
// get before it starts over.
const LogCapInterval = 10 * time.Second

// CapLogs empties every *.log in dir that has reached max, and notes on the
// first line that it did. Emptied in place rather than removed: the processes
// writing them — sing-box, launchd's capture of a daemon's output, the client
// itself — keep the file open in append mode and carry on at its start, where
// removing it would send their writes nowhere until they were restarted.
func CapLogs(dir string, max int64) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() < max {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if err := os.Truncate(path, 0); err != nil {
			continue
		}
		if f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0); err == nil {
			_, _ = fmt.Fprintf(f, "[%s] log started over at %d MB\n", time.Now().Format("2006-01-02 15:04:05"), max>>20)
			_ = f.Close()
		}
	}
}

// CapLogsEvery keeps the logs in dir under MaxLogBytes for as long as the
// process runs.
func CapLogsEvery(dir string) {
	go func() {
		for {
			CapLogs(dir, MaxLogBytes)
			time.Sleep(LogCapInterval)
		}
	}()
}
