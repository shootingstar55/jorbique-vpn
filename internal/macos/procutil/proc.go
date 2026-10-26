package procutil

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/sysexec"
)

const sudoExecutable = "/usr/bin/sudo"

// Which finds an executable analogous to `which` shell builtin, including Homebrew fallbacks.
func Which(tool string) string {
	if strings.HasPrefix(tool, "/") {
		if isExecutable(tool) {
			return tool
		}
		return ""
	}
	envPath := os.Getenv("PATH")
	for _, dir := range strings.Split(envPath, ":") {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, tool)
		if isExecutable(candidate) {
			return candidate
		}
	}
	for _, candidate := range []string{
		"/opt/homebrew/bin/" + tool,
		"/opt/homebrew/sbin/" + tool,
		"/usr/local/bin/" + tool,
		"/usr/local/sbin/" + tool,
	} {
		if isExecutable(candidate) {
			return candidate
		}
	}
	return ""
}

func isExecutable(path string) bool {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return false
	}
	return st.Mode()&0o111 != 0
}

// RunProcess runs a command and returns the exit status, optionally via sudo when not root.
func RunProcess(launchPath string, args []string, asRoot bool) (int, error) {
	var cmd *exec.Cmd
	if asRoot && os.Geteuid() != 0 {
		cmd = sysexec.Command(sudoExecutable, append([]string{"-n", launchPath}, args...)...)
	} else {
		// launchPath comes from app-managed install locations (audited chokepoint).
		cmd = sysexec.Command(launchPath, args...)
	}
	var errBuf bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			status := ee.ExitCode()
			errStr := strings.TrimSpace(errBuf.String())
			if asRoot && os.Geteuid() != 0 && containsCI(errStr, "password") {
				return status, core.Permission("sudo requires password (need NOPASSWD). " + errStr)
			}
			return status, nil
		}
		return 127, core.StartFailed(launchPath + " " + strings.Join(args, " "))
	}
	return 0, nil
}

func containsCI(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

// RunAndCapture runs a command and captures stdout. Returns (status, output).
func RunAndCapture(launchPath string, args ...string) (int, string) {
	// launchPath comes from app-managed install locations (audited chokepoint).
	cmd := sysexec.Command(launchPath, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode(), out.String()
		}
		return 127, ""
	}
	return 0, out.String()
}

// RunAndCaptureInUserSession runs a command via launchctl asuser + sudo -u.
func RunAndCaptureInUserSession(uid int, user string, launchPath string, args ...string) (int, string) {
	return runInUserSession(uid, user, launchPath, "", args...)
}

// RunWithStdinInUserSession is RunAndCaptureInUserSession for a command that
// takes a payload — credentials on their way into the console user's keychain,
// which is the one account that can write it.
func RunWithStdinInUserSession(uid int, user string, launchPath string, stdinText string, args ...string) (int, string) {
	return runInUserSession(uid, user, launchPath, stdinText, args...)
}

func runInUserSession(uid int, user string, launchPath string, stdinText string, args ...string) (int, string) {
	full := []string{"asuser", strconv.Itoa(uid), sudoExecutable, "-n", "-u", user, launchPath}
	full = append(full, args...)
	cmd := sysexec.Command("/bin/launchctl", full...)
	if stdinText != "" {
		cmd.Stdin = strings.NewReader(stdinText)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode(), strings.TrimSpace(out.String())
		}
		return 127, ""
	}
	return 0, strings.TrimSpace(out.String())
}

func GetConsoleUser() (int, string, bool) {
	if os.Geteuid() != 0 {
		return 0, "", false
	}
	uid, name, ok := ConsoleUserOf("/dev/console")
	if !ok || uid <= 0 || name == "" || name == "root" {
		return 0, "", false
	}
	return uid, name, true
}

// ConsoleUserOf reads who owns the console device — the GUI login user. This
// used to run /usr/bin/stat and parse its output; the same answer is a stat(2)
// call and a passwd lookup, which is what stat does.
func ConsoleUserOf(consolePath string) (int, string, bool) {
	fi, err := os.Stat(consolePath)
	if err != nil {
		return 0, "", false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return 0, "", false
	}
	uid := int(st.Uid)
	u, err := user.LookupId(strconv.Itoa(uid))
	if err != nil {
		return uid, "", false
	}
	return uid, u.Username, true
}

// ReadPID reads a pid file.
func ReadPID(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	s := strings.TrimSpace(string(data))
	if s == "" {
		return 0, false
	}
	pid, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return pid, true
}

func WritePID(pid int, path string) {
	_ = os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0o644)
}

// IsAlive checks whether a process exists by sending signal 0.
// On Unix, kill(pid, 0) returns EPERM when the pid exists but belongs to another
// uid (e.g. menu bar runs status as the user while sing-box was started under sudo
// as root). EPERM still means the process is present.
func IsAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}

// CommandLine returns the process command line for diagnostics / stale pid checks.
func CommandLine(pid int) string {
	if pid <= 0 {
		return ""
	}
	st, out := RunAndCapture("/bin/ps", "-p", strconv.Itoa(pid), "-o", "command=")
	if st != 0 {
		return ""
	}
	return strings.TrimSpace(out)
}

// Terminate sends SIGTERM and falls back to SIGKILL after timeout.
func Terminate(pid int, timeout time.Duration) {
	_ = syscall.Kill(pid, syscall.SIGTERM)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !IsAlive(pid) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

// LastLines returns the last N lines of a file (or full content if smaller).
func LastLines(path string, count int) string {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return "(log file empty or unreadable)"
	}
	s := string(data)
	lines := strings.Split(s, "\n")
	if len(lines) <= count {
		return s
	}
	return strings.Join(lines[len(lines)-count:], "\n")
}

// Spawn starts a process redirecting stdout/stderr to logURL and writing pid.
// Returns the child PID. When asRoot=true and not currently root, runs via sudo -n.
func Spawn(launchPath string, args []string, logPath string, pidPath string, asRoot bool) (int, error) {
	return SpawnWithEnvironment(launchPath, args, logPath, pidPath, asRoot, nil)
}

// SpawnWithEnvironment uses an explicit environment when supplied.
func SpawnWithEnvironment(launchPath string, args []string, logPath string, pidPath string, asRoot bool, environment []string) (int, error) {
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, core.StartFailed("open log file failed: " + err.Error())
	}
	var cmd *exec.Cmd
	if asRoot && os.Geteuid() != 0 {
		cmd = sysexec.Command(sudoExecutable, append([]string{"-n", launchPath}, args...)...)
	} else {
		// launchPath comes from app-managed install locations (audited chokepoint).
		cmd = sysexec.Command(launchPath, args...)
	}
	cmd.Stdout = logFile
	cmd.Env = environment
	cmd.Stderr = logFile
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return 0, core.StartFailed(launchPath + " " + strings.Join(args, " "))
	}
	pid := cmd.Process.Pid
	WritePID(pid, pidPath)
	// Detach so child outlives us.
	go func() {
		_ = cmd.Wait()
		_ = logFile.Close()
	}()
	return pid, nil
}
