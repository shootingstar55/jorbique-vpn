//go:build darwin

package applekit

import (
	"testing"
)

func TestConsoleUserDoesNotCrash(t *testing.T) {
	// ConsoleUser depends on the environment (GUI session vs headless CI), so we
	// only assert it returns without panicking and never yields a placeholder.
	u := ConsoleUser()
	t.Logf("ConsoleUser() = %q", u)
	if u == "loginwindow" || u == "_mbsetupuser" {
		t.Errorf("ConsoleUser returned a placeholder: %q", u)
	}
}
