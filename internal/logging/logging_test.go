package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func resetForTest() {
	mu.Lock()
	logFile = ""
	initialized = false
	mu.Unlock()
}

func TestWriteBeforeInitializeIsNoop(t *testing.T) {
	resetForTest()
	Write("should be dropped", "INFO") // must not panic and must not create files
	if initialized {
		t.Error("Write must not initialize logging")
	}
}

func TestInitializeEmptyRootIsNoop(t *testing.T) {
	resetForTest()
	Initialize("controller.log", "")
	if initialized {
		t.Error("empty projectRoot should be a no-op")
	}
}

func TestInitializeWriteAndClear(t *testing.T) {
	resetForTest()
	dir := t.TempDir()
	Initialize("controller.log", dir)

	logPath := filepath.Join(dir, "logs", "controller.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("log file not created: %v", err)
	}
	if !strings.Contains(string(data), "log started") {
		t.Errorf("missing header: %q", data)
	}

	Write("hello world", "INFO")
	data, _ = os.ReadFile(logPath)
	if !strings.Contains(string(data), "hello world") || !strings.Contains(string(data), "[INFO]") {
		t.Errorf("Write did not append entry: %q", data)
	}

	// Re-initializing the same already-active path is a no-op (no truncation, no marker).
	Initialize("controller.log", dir)
	data, _ = os.ReadFile(logPath)
	if !strings.Contains(string(data), "hello world") {
		t.Error("re-Initialize truncated existing log")
	}

	// Initializing again after a state reset, with the file already non-empty,
	// appends a session marker instead of rewriting the header.
	resetForTest()
	Initialize("controller.log", dir)
	data, _ = os.ReadFile(logPath)
	if !strings.Contains(string(data), "hello world") {
		t.Error("session re-init truncated existing log")
	}
	if !strings.Contains(string(data), "Log session at") {
		t.Error("re-Initialize on a non-empty file should append a session marker")
	}

	ClearAll(dir)
	if _, err := os.Stat(logPath); err == nil {
		t.Error("ClearAll did not remove log file")
	}

	// ClearAll on a missing logs dir is a no-op.
	ClearAll(t.TempDir())
}
