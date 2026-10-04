package control

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The operations that carry a payload — route rules, sing-box settings,
// credentials — are run in a process of the daemon's own, so their input has to
// travel with the request. Nothing tested that it arrives, and the whole point
// of those commands is what is in it.
func TestThePayloadReachesTheOperation(t *testing.T) {
	dir := shortTempDir(t)
	exe := filepath.Join(dir, "fake-client")
	// Echoes what it was given on stdin, so the test sees exactly what arrived.
	// `cat` rather than a read loop: the payload has no trailing newline — the
	// menu bar sends "<uuid>\n<tags>" — and `while read` drops a final line
	// without one, which would make this fail on the shell rather than on
	// anything the daemon did.
	script := "#!/bin/sh\ncat\n"
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
	var out, errOut bytes.Buffer
	code, err := client.Run(Request{
		Command: "set-singbox-config",
		Stdin:   "7998568d-7ae4-4f65-a4cb-b20e78af6a87\ntuic,hysteria2",
	}, &out, &errOut)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Errorf("exit = %d, stderr = %q", code, errOut.String())
	}
	// Both lines, including the last one that carries no newline of its own.
	for _, want := range []string{"7998568d-7ae4-4f65-a4cb-b20e78af6a87", "tuic,hysteria2"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("stdout = %q, want it to contain %q", out.String(), want)
		}
	}
}

// An operation that takes no payload must get none — a child left waiting on an
// stdin that never closes is a wedged privileged process.
func TestAnOperationWithoutAPayloadGetsClosedInput(t *testing.T) {
	dir := shortTempDir(t)
	exe := filepath.Join(dir, "fake-client")
	// Reads stdin to the end; with nothing attached this returns at once, and
	// if it were left open the operation would never finish.
	script := "#!/bin/sh\ncat >/dev/null\necho done\n"
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
	done := make(chan struct{})
	var out bytes.Buffer
	var code int
	var runErr error
	go func() {
		defer close(done)
		code, runErr = client.Run(Request{Command: "start", Args: []string{"intra"}}, &out, &out)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the operation never finished: its stdin was left open")
	}
	if runErr != nil {
		t.Fatalf("Run: %v", runErr)
	}
	if code != 0 || !strings.Contains(out.String(), "done") {
		t.Errorf("exit = %d, out = %q", code, out.String())
	}
}
