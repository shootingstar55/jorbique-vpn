package control

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

// ErrNoDaemon means the socket is not there or nothing is listening on it: the
// daemon is not running. Callers tell it apart from a refused request, because
// the two ask for different things from the person reading the message.
var ErrNoDaemon = errors.New("control: the privileged daemon is not running")

// Client talks to the daemon from an unprivileged process — the menu bar, or
// the binary run from a terminal by a user who is in the admin group.
type Client struct {
	SocketPath string
	// Timeout bounds connecting, not the operation: starting a tunnel can take
	// a while, and the daemon streams output the whole time.
	Timeout time.Duration
	// Idle bounds how long the daemon may say nothing at all. Zero uses
	// defaultIdleTimeout. An operation is unbounded on purpose, but a daemon
	// that has stopped producing output and stopped finishing is wedged, and
	// waiting on it forever leaves the caller wedged too. The clock restarts on
	// every line, so a long operation that is still talking is never cut off.
	Idle time.Duration
}

// defaultIdleTimeout is far longer than any gap between an operation's output
// lines and far shorter than "for ever".
const defaultIdleTimeout = 5 * time.Minute

// Available reports whether the daemon is there to talk to.
func (c Client) Available() bool {
	conn, err := c.dial()
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (c Client) dial() (net.Conn, error) {
	if strings.TrimSpace(c.SocketPath) == "" {
		return nil, ErrNoDaemon
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	conn, err := net.DialTimeout("unix", c.SocketPath, timeout)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoDaemon
		}
		var oe *net.OpError
		if errors.As(err, &oe) {
			return nil, ErrNoDaemon
		}
		return nil, err
	}
	return conn, nil
}

// Run sends one operation and copies its output to the given writers as it
// arrives, returning the exit status the daemon reported.
func (c Client) Run(req Request, stdout, stderr io.Writer) (int, error) {
	conn, err := c.dial()
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	// A daemon that accepted the connection but is not reading must not hold
	// the caller here; sending a request is one small write.
	_ = conn.SetWriteDeadline(time.Now().Add(requestReadTimeout))
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return 0, err
	}
	// Nothing more is sent, and the daemon reads exactly one request — close
	// our half so it does not wait for an end it will never see.
	if uc, ok := conn.(*net.UnixConn); ok {
		_ = uc.CloseWrite()
	}

	idle := c.Idle
	if idle <= 0 {
		idle = defaultIdleTimeout
	}
	dec := json.NewDecoder(conn)
	for {
		// Reset on every message, so this bounds silence rather than the
		// operation.
		_ = conn.SetReadDeadline(time.Now().Add(idle))
		var resp Response
		if err := dec.Decode(&resp); err != nil {
			if errors.Is(err, io.EOF) {
				// The daemon went away mid-operation; the caller cannot know
				// whether it finished, so this is not a success.
				return 0, ErrNoDaemon
			}
			return 0, err
		}
		if resp.Exit != nil {
			return *resp.Exit, nil
		}
		writeLine(resp, stdout, stderr)
	}
}

// writeLine copies one output line to the writer of the stream it came from.
func writeLine(resp Response, stdout, stderr io.Writer) {
	w := stdout
	if resp.Stderr {
		w = stderr
	}
	if w != nil && resp.Line != "" {
		_, _ = io.WriteString(w, resp.Line+"\n")
	}
}
