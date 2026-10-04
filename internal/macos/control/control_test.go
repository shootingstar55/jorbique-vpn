package control

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOnlyTheOperationsWeListAreAccepted(t *testing.T) {
	for _, cmd := range []string{"start", "stop", "set-route-rules"} {
		if err := ValidateRequest(Request{Command: cmd, Args: []string{"Jorbique VPN"}}); err != nil {
			t.Errorf("%s should be accepted: %v", cmd, err)
		}
	}
	// Not a VPN operation, however much it looks like one of our subcommands.
	for _, cmd := range []string{"", "--daemon", "status", "help", "install-service", "renew-certs", "sh", "start; rm -rf /"} {
		if err := ValidateRequest(Request{Command: cmd}); err == nil {
			t.Errorf("%q was accepted", cmd)
		}
	}
}

// The arguments reach a process running as root. Anything that could name a
// file, escape the argument list, or run a second command is refused outright.
func TestArgumentsThatCouldNameAPathOrACommandAreRefused(t *testing.T) {
	bad := []string{
		"/etc/passwd",
		"../../etc/sudoers",
		"intra/../..",
		"-rf",
		"--config=/tmp/x",
		"intra; rm -rf /",
		"intra\nstop",
		"intra`whoami`",
		"$(whoami)",
		"intra\x00",
		strings.Repeat("a", 100),
		"",
	}
	for _, a := range bad {
		if err := ValidateRequest(Request{Command: "start", Args: []string{a}}); err == nil {
			t.Errorf("accepted argument %q", a)
		}
	}
	good := []string{"intra", "Jorbique VPN", "jorbique-prod", "all", "force", "projects_2"}
	for _, a := range good {
		if err := ValidateRequest(Request{Command: "start", Args: []string{a}}); err != nil {
			t.Errorf("refused a real name %q: %v", a, err)
		}
	}
}

func TestOversizedRequestsAreRefused(t *testing.T) {
	many := make([]string, 32)
	for i := range many {
		many[i] = "intra"
	}
	if err := ValidateRequest(Request{Command: "start", Args: many}); err == nil {
		t.Error("accepted an unbounded argument list")
	}
	if err := ValidateRequest(Request{Command: "set-route-rules", Stdin: strings.Repeat("x", 70*1024)}); err == nil {
		t.Error("accepted an unbounded payload")
	}
}

// No operation the daemon runs reads a flag, so none is accepted.
func TestAFlagIsRefused(t *testing.T) {
	if err := ValidateRequest(Request{Command: "start", Args: []string{"--force"}}); err == nil {
		t.Error("start accepted --force, which it does not read")
	}
}

// The round trip: a client with no privilege of its own names an operation, the
// server runs it and streams back what it printed and how it ended.
func TestTheDaemonRunsTheOperationAndReportsHowItEnded(t *testing.T) {
	dir := shortTempDir(t)
	exe := filepath.Join(dir, "fake-client")
	script := `#!/bin/sh
echo "running $1 for ${2:-nothing}"
echo "a warning" >&2
[ "$2" = "broken" ] && exit 3
exit 0
`
	if err := os.WriteFile(exe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	srv := &Server{Executable: exe}
	sock := filepath.Join(dir, SocketName)
	if err := srv.Listen(sock); err != nil {
		t.Fatalf("Listen: %v", err)
	}
	go srv.Serve()
	defer srv.Close()

	client := Client{SocketPath: sock, Timeout: 2 * time.Second}
	if !client.Available() {
		t.Fatal("the socket is not reachable")
	}

	var out, errOut bytes.Buffer
	code, err := client.Run(Request{Command: "start", Args: []string{"intra"}}, &out, &errOut)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "running start for intra") {
		t.Errorf("stdout = %q", out.String())
	}
	if !strings.Contains(errOut.String(), "a warning") {
		t.Errorf("stderr = %q", errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code, err = client.Run(Request{Command: "start", Args: []string{"broken"}}, &out, &errOut)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 3 {
		t.Errorf("exit = %d, want the 3 the operation returned", code)
	}
}

// A refused request must not run anything, and must say so on the connection
// rather than by hanging up.
func TestARefusedRequestRunsNothing(t *testing.T) {
	dir := shortTempDir(t)
	marker := filepath.Join(dir, "it-ran")
	exe := filepath.Join(dir, "fake-client")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	srv := &Server{Executable: exe}
	sock := filepath.Join(dir, SocketName)
	if err := srv.Listen(sock); err != nil {
		t.Fatalf("Listen: %v", err)
	}
	go srv.Serve()
	defer srv.Close()

	var out, errOut bytes.Buffer
	code, err := Client{SocketPath: sock, Timeout: 2 * time.Second}.
		Run(Request{Command: "start", Args: []string{"/etc/passwd"}}, &out, &errOut)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code == 0 {
		t.Error("a refused request reported success")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Error("the operation ran anyway")
	}
	if !strings.Contains(errOut.String(), "will not take") {
		t.Errorf("no reason given: %q", errOut.String())
	}
}

// The socket is the privilege boundary: root's, and open to the admin group
// only. World-writable would hand every account on the machine what the sudoers
// drop-in used to hand them.
func TestTheSocketIsNotOpenToEveryone(t *testing.T) {
	dir := shortTempDir(t)
	srv := &Server{Executable: "/bin/echo"}
	sock := filepath.Join(dir, SocketName)
	if err := srv.Listen(sock); err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer srv.Close()

	st, err := os.Stat(sock)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm()&0o007 != 0 {
		t.Errorf("socket mode %v lets anyone connect", st.Mode().Perm())
	}
}

// Nothing listening is its own answer: the caller has to tell "the daemon is
// not running" apart from "the daemon refused", because only one of the two is
// worth reporting to the user as something to fix.
func TestNoDaemonIsReportedAsSuch(t *testing.T) {
	c := Client{SocketPath: filepath.Join(t.TempDir(), "absent.sock"), Timeout: time.Second}
	if c.Available() {
		t.Fatal("reported a daemon that is not there")
	}
	if _, err := c.Run(Request{Command: "start"}, nil, nil); err != ErrNoDaemon {
		t.Errorf("err = %v, want ErrNoDaemon", err)
	}
}

// shortTempDir is t.TempDir() with a path a unix socket fits in: sun_path holds
// 104 bytes on macOS, and the per-test directory under /var/folders eats most
// of them. The real socket lives in /var/run/jorbique-vpn and is nowhere near
// the limit.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "mvctl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
