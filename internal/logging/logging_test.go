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
	if info, err := os.Stat(logPath); err != nil || info.Size() != 0 {
		t.Errorf("ClearAll should leave the log in place and empty: %v", err)
	}

	// ClearAll on a missing logs dir is a no-op.
	ClearAll(t.TempDir())
}

func TestReadTailShowsTheNewestWholeLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.log")
	var b strings.Builder
	for i := 0; i < 100; i++ {
		b.WriteString("line-")
		b.WriteString(strings.Repeat("x", 10))
		b.WriteString("\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadTail(path, 50)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if !strings.HasPrefix(lines[0], "…") {
		t.Fatalf("a cut log should say so on top, got %q", lines[0])
	}
	for _, l := range lines[1:] {
		if l != "line-xxxxxxxxxx" {
			t.Errorf("tail starts mid-line or is garbled: %q", l)
		}
	}
}

func TestReadTailSmallAndMissingFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "small.log")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadTail(path, 1<<20); err != nil || got != "one\ntwo\n" {
		t.Errorf("small log = %q, %v", got, err)
	}
	// A log Clear emptied away reads as empty, not as an error.
	if got, err := ReadTail(filepath.Join(dir, "gone.log"), 1<<20); err != nil || got != "" {
		t.Errorf("missing log = %q, %v", got, err)
	}
}

func TestCapLogsEmptiesOnlyLogsPastTheCap(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "sing-box.log")
	small := filepath.Join(dir, "app.log")
	other := filepath.Join(dir, "config.json")
	for path, size := range map[string]int{big: 200, small: 10, other: 200} {
		if err := os.WriteFile(path, []byte(strings.Repeat("a", size)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	CapLogs(dir, 100)
	data, _ := os.ReadFile(big)
	if strings.Contains(string(data), "aaaa") || !strings.Contains(string(data), "started over") {
		t.Errorf("log past the cap was not started over: %q", data)
	}
	if data, _ := os.ReadFile(small); len(data) != 10 {
		t.Errorf("log under the cap changed: %q", data)
	}
	if data, _ := os.ReadFile(other); len(data) != 200 {
		t.Error("only *.log files are capped")
	}
}
