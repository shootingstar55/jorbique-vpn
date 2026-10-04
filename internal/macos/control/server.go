package control

import (
	"bufio"
	"encoding/json"
	"io"
	"jorbiquevpn/internal/sysexec"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// requestReadTimeout bounds how long a connection may take to send its request.
// Sending one is a single small write; anything slower is a client that is not
// going to send one at all.
const requestReadTimeout = 15 * time.Second

// replyWriteTimeout bounds a single write back to the client. A caller that has
// gone away — the menu bar's watchdog gave up, the terminal was closed — leaves
// a socket nobody drains, and without this the operation's output pump blocks
// on it for as long as the daemon lives.
const replyWriteTimeout = 30 * time.Second

// controlPrefix marks a reply as the daemon's own failure to run the operation.
const controlPrefix = "control: "

// Server is the daemon's side of the socket. It runs each accepted request as a
// child of the daemon — this binary, the command from the allowlist, the
// validated arguments — and streams back what it prints.
//
// Running a child rather than doing the work in the daemon's own goroutines
// keeps a wedged operation from taking the daemon with it, and keeps the
// daemon's state out of reach of the request.
type Server struct {
	// Executable is the binary the daemon runs operations with: its own. Not
	// taken from the request, and there is no field that would let it be.
	Executable string
	// Logf reports accepted and refused requests. Optional.
	Logf func(string)

	listener net.Listener
	wg       sync.WaitGroup
}

// Listen opens the control socket. It is created root-owned and readable and
// writable by the admin group only — the same set of people who could run the
// privileged subcommands through sudo before, and nobody else.
func (s *Server) Listen(socketPath string) error {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return err
	}
	// A socket left by a daemon that did not shut down cleanly would make Listen
	// fail with "address already in use"; nothing else owns this path.
	_ = os.Remove(socketPath)

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	if err := os.Chmod(socketPath, 0o660); err != nil {
		_ = ln.Close()
		return err
	}
	if gid, ok := adminGroupID(); ok {
		_ = os.Chown(socketPath, 0, gid)
	}
	s.listener = ln
	return nil
}

// Serve accepts connections until the listener is closed.
func (s *Server) Serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer conn.Close()
			s.handle(conn)
		}()
	}
}

// Close stops accepting and waits for the requests in flight.
func (s *Server) Close() error {
	if s.listener == nil {
		return nil
	}
	err := s.listener.Close()
	s.wg.Wait()
	return err
}

func (s *Server) handle(conn net.Conn) {
	enc := json.NewEncoder(deadlineWriter{conn: conn, timeout: replyWriteTimeout})
	var req Request
	// One request per connection, and a bounded one: a client that opens the
	// socket and never stops writing must not grow the daemon.
	//
	// Bounded in time as well as in size. Anything on the machine can connect
	// to the socket; one that connects and then says nothing used to hold a
	// daemon goroutine for as long as it cared to. Sending a request is not
	// slow, so this is short. The deadline is dropped once the request is in —
	// what follows is the operation, and a tunnel takes as long as it takes.
	_ = conn.SetReadDeadline(time.Now().Add(requestReadTimeout))
	if err := json.NewDecoder(io.LimitReader(conn, 128*1024)).Decode(&req); err != nil {
		s.reply(enc, "control: unreadable request", 2)
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	if err := ValidateRequest(req); err != nil {
		s.log("refused: " + err.Error())
		s.reply(enc, err.Error(), 2)
		return
	}
	s.log("running " + req.Command)
	s.run(enc, req)
}

func (s *Server) run(enc *json.Encoder, req Request) {
	cmd := sysexec.Command(s.Executable, append([]string{req.Command}, req.Args...)...)
	if req.Stdin != "" {
		cmd.Stdin = stringReader(req.Stdin)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.reply(enc, controlPrefix+err.Error(), 2)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		s.reply(enc, controlPrefix+err.Error(), 2)
		return
	}
	if err := cmd.Start(); err != nil {
		s.reply(enc, controlPrefix+err.Error(), 2)
		return
	}

	var streams sync.WaitGroup
	var mu sync.Mutex
	pump := func(r io.Reader, isErr bool) {
		defer streams.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			mu.Lock()
			_ = enc.Encode(Response{Line: sc.Text(), Stderr: isErr})
			mu.Unlock()
		}
	}
	streams.Add(2)
	go pump(stdout, false)
	go pump(stderr, true)
	streams.Wait()

	code := 0
	if err := cmd.Wait(); err != nil {
		code = 1
		var ee *exec.ExitError
		if asExit(err, &ee) {
			code = ee.ExitCode()
		}
	}
	mu.Lock()
	_ = enc.Encode(Response{Exit: &code})
	mu.Unlock()
}

// deadlineWriter puts a deadline on every write to the connection. The output
// of an operation is written as it arrives, and there is no bound on how long
// an operation runs — so the bound has to be per write rather than overall.
type deadlineWriter struct {
	conn    net.Conn
	timeout time.Duration
}

func (w deadlineWriter) Write(p []byte) (int, error) {
	_ = w.conn.SetWriteDeadline(time.Now().Add(w.timeout))
	return w.conn.Write(p)
}

func (s *Server) reply(enc *json.Encoder, line string, code int) {
	_ = enc.Encode(Response{Line: line, Stderr: true})
	_ = enc.Encode(Response{Exit: &code})
}

func (s *Server) log(msg string) {
	if s.Logf != nil {
		s.Logf(msg)
	}
}

// adminGroupID resolves the admin group, which owns the socket.
func adminGroupID() (int, bool) {
	g, err := user.LookupGroup("admin")
	if err != nil {
		return 0, false
	}
	id, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, false
	}
	return id, true
}
