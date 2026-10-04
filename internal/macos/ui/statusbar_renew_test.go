//go:build darwin

package ui

import (
	"strings"
	"testing"
)

func TestRunAndCaptureOutErrCombines(t *testing.T) {
	rec := &recorder{def: cmdResult{stdout: "cert renew [intra]: download: i/o timeout\n", stderr: "sudo: a password is required\n", status: 1}}
	r := &runner{run: rec.run}
	out, status := r.runAndCaptureOutErr("/bin/exe", []string{"renew-certs"})
	if status != 1 {
		t.Errorf("status = %d", status)
	}
	if !strings.Contains(out, "i/o timeout") || !strings.Contains(out, "password is required") {
		t.Errorf("combined output lost a stream: %q", out)
	}
	if len(rec.calls) != 1 || !rec.calls[0].captureStdout || !rec.calls[0].captureStderr {
		t.Errorf("expected one call capturing both streams, got %+v", rec.calls)
	}
}
