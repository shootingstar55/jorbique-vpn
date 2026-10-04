package vpnmgr

import "jorbiquevpn/internal/macos/control"

// The daemon is the client's one privileged component. Everything else — the
// menu bar, the binary run from a terminal — asks it for the operations that
// need root, over a socket in the run directory, and the daemon runs each one
// as a child of itself. What may be asked is control.operations; what the
// arguments may look like is control.ValidateRequest.
//
// Before this the privileged path was a sudoers drop-in, which is a way of
// saying that anything running as the logged-in user could run our binary as
// root with any subcommand at all.

// startControlServer opens the control socket and serves it for as long as the
// daemon runs. A daemon that cannot open it keeps going: the tunnels it is
// watching matter more than the requests it cannot take, and the menu bar says
// plainly when there is nothing to talk to.
func (m *Manager) startControlServer() *control.Server {
	srv := &control.Server{
		Executable: m.Paths.ExecutablePath(),
		Logf:       func(msg string) { m.Logger.Debug("control: " + msg) },
	}
	path := control.SocketPath(m.Paths.RunDir)
	if err := srv.Listen(path); err != nil {
		m.Logger.Error("control: cannot listen on " + path + ": " + err.Error())
		return nil
	}
	m.Logger.Log("control: listening on " + path)
	go srv.Serve()
	return srv
}
