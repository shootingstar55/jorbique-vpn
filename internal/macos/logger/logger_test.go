package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shrinkCaps lowers the trim thresholds for a test and restores them after.
func shrinkCaps(t *testing.T, max, keep int64) {
	t.Helper()
	origMax, origKeep := maxLogBytes, keepLogBytes
	maxLogBytes, keepLogBytes = max, keep
	t.Cleanup(func() { maxLogBytes, keepLogBytes = origMax, origKeep })
}

func TestLogStaysUnderCap(t *testing.T) {
	shrinkCaps(t, 4096, 1024)
	path := filepath.Join(t.TempDir(), "app.log")
	l := New(path)

	for i := 0; i < 500; i++ {
		l.Debug(strings.Repeat("x", 100))
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// One line of headroom: the trim happens before a write, so the file may
	// briefly sit at keep+line bytes, never anywhere near the old unbounded growth.
	if fi.Size() >= maxLogBytes {
		t.Fatalf("log size %d not kept under cap %d", fi.Size(), maxLogBytes)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "log trimmed to") {
		t.Fatal("trimmed log lacks the trim marker")
	}
	// The newest lines survive; every kept line is intact (starts at a line
	// boundary with the timestamp bracket).
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	for i, ln := range lines {
		if !strings.HasPrefix(ln, "[") {
			t.Fatalf("line %d is not aligned to a line start: %q", i, ln)
		}
	}
	if !strings.Contains(lines[len(lines)-1], strings.Repeat("x", 100)) {
		t.Fatalf("newest line missing from trimmed log: %q", lines[len(lines)-1])
	}
}

func TestTrimKeepsAppendHandleWorking(t *testing.T) {
	shrinkCaps(t, 2048, 512)
	path := filepath.Join(t.TempDir(), "app.log")
	l := New(path)

	for i := 0; i < 100; i++ {
		l.Debug(strings.Repeat("y", 64))
	}
	l.Debug("sentinel-after-trim")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "sentinel-after-trim") {
		t.Fatal("write after trim did not land in the log")
	}
}

// Lines longer than the keep budget are the case where alignment used to
// break: the window then holds exactly one newline — the one ending the file —
// and cutting there is what keeps the log free of headless fragments. A caller
// logging a stack trace or a JSON body is all it takes to hit this in
// production, where the budget is 2.5MB.
func TestTrimAlignsWhenLinesExceedKeepBudget(t *testing.T) {
	shrinkCaps(t, 300, 50)
	path := filepath.Join(t.TempDir(), "app.log")
	l := New(path)

	for i := 0; i < 4; i++ {
		l.Debug(strings.Repeat("x", 200))
	}
	l.Debug(strings.Repeat("y", 400))

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, ln := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if ln == "" {
			continue
		}
		if !strings.HasPrefix(ln, "[") {
			t.Fatalf("line %d is not aligned to a line start: %.40q…", i, ln)
		}
	}
	if !strings.Contains(string(data), "log trimmed to") {
		t.Fatal("expected a trim to have happened")
	}
}

func TestNoTrimBelowCap(t *testing.T) {
	shrinkCaps(t, 1<<20, 1<<19)
	path := filepath.Join(t.TempDir(), "app.log")
	l := New(path)
	l.Debug("only line")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "log trimmed") {
		t.Fatal("small log was trimmed")
	}
	if !strings.Contains(string(data), "only line") {
		t.Fatal("line missing")
	}
}
