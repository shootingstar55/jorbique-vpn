package control

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// An operation the daemon cannot start is answered on the connection, as the
// daemon's own failure, with a non-zero exit — not with a hang-up.
func TestAnOperationThatCannotStartIsReportedAsTheDaemons(t *testing.T) {
	var buf bytes.Buffer
	(&Server{Executable: "relative/fake-client"}).run(json.NewEncoder(&buf), Request{Command: "start"})

	dec := json.NewDecoder(&buf)
	var line, exit Response
	if err := dec.Decode(&line); err != nil {
		t.Fatal(err)
	}
	if err := dec.Decode(&exit); err != nil {
		t.Fatal(err)
	}
	if !line.Stderr || !strings.HasPrefix(line.Line, controlPrefix) {
		t.Errorf("first reply = %+v, want a %q line on stderr", line, controlPrefix)
	}
	if exit.Exit == nil || *exit.Exit != 2 {
		t.Errorf("exit reply = %+v, want exit 2", exit)
	}
}
