package logger

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"jorbiquevpn/internal/macos/ansiutil"
)

// maxLogBytes caps app.log; once a write finds the file at or past the cap it
// is trimmed in place down to the newest keepLogBytes so the log keeps rolling
// instead of growing forever. Vars so tests can shrink them.
var (
	maxLogBytes  int64 = 5 << 20
	keepLogBytes int64 = (5 << 20) / 2
)

type Logger struct {
	path string
	mu   sync.Mutex
	f    *os.File
}

func New(path string) *Logger {
	dir := filepath.Dir(path)
	_ = os.MkdirAll(dir, 0o755)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if f, err := os.Create(path); err == nil {
			_ = f.Close()
		}
	}
	return &Logger{path: path}
}

func (l *Logger) ts() string {
	return time.Now().Format("2006-01-02T15:04:05Z07:00")
}

func (l *Logger) writeLine(line string) {
	plain := ansiutil.Strip(line)
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.openLocked() {
		return
	}
	if fi, err := l.f.Stat(); err == nil && fi.Size() >= maxLogBytes {
		l.trimLocked()
		if !l.openLocked() {
			return
		}
	}
	_, _ = l.f.WriteString(plain)
}

func (l *Logger) openLocked() bool {
	if l.f != nil {
		return true
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return false
	}
	l.f = f
	return true
}

// trimLocked rewrites app.log in place, keeping the newest keepLogBytes
// (aligned to a line start) behind a trim marker. In place — same inode — on
// purpose: the daemon, the menu bar and CLI subcommands all append to this
// file, and a rename-based rotation would strand their open O_APPEND handles
// on the old file. A concurrent trim by another process can drop a few lines;
// for a debug log that beats coordinating locks across processes.
func (l *Logger) trimLocked() {
	data, err := os.ReadFile(l.path)
	if err != nil || int64(len(data)) < maxLogBytes {
		return // unreadable, or another process already trimmed
	}
	// Keep whole lines only: cut everything before the first line boundary in
	// the window. When the window holds no boundary — a single line longer than
	// the budget — keep nothing rather than a headless fragment of it: a line
	// without its [timestamp] reads as if it belonged to the entry above.
	tail := data[int64(len(data))-keepLogBytes:]
	if i := bytes.IndexByte(tail, '\n'); i >= 0 {
		tail = tail[i+1:]
	} else {
		tail = nil
	}
	marker := fmt.Sprintf("[%s] log trimmed to %dB (cap %dB)\n", l.ts(), keepLogBytes, maxLogBytes)
	if err := os.WriteFile(l.path, append([]byte(marker), tail...), 0o644); err != nil {
		return
	}
	_ = l.f.Close()
	l.f = nil
}

func (l *Logger) Log(msg string) {
	line := fmt.Sprintf("[%s] %s\n", l.ts(), msg)
	fmt.Fprint(os.Stdout, line)
	l.writeLine(line)
}

func (l *Logger) Warn(msg string) {
	line := fmt.Sprintf("[%s] WARN: %s\n", l.ts(), msg)
	fmt.Fprint(os.Stderr, ansiutil.Yellow(line))
	l.writeLine(line)
}

func (l *Logger) Error(msg string) {
	line := fmt.Sprintf("[%s] ERROR: %s\n", l.ts(), msg)
	fmt.Fprint(os.Stderr, ansiutil.Red(line))
	l.writeLine(line)
}

func (l *Logger) Debug(msg string) {
	l.writeLine(fmt.Sprintf("[%s] DEBUG: %s\n", l.ts(), msg))
}

func (l *Logger) Event(event string, reason string, pairs ...[2]string) {
	parts := []string{"event=" + event}
	if reason != "" {
		parts = append(parts, "reason="+reason)
	}
	for _, p := range pairs {
		if p[0] != "" {
			parts = append(parts, p[0]+"="+p[1])
		}
	}
	line := fmt.Sprintf("[%s] %s\n", l.ts(), strings.Join(parts, " "))
	fmt.Fprint(os.Stdout, line)
	l.writeLine(line)
}
