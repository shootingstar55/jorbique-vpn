package sysexec

import (
	"context"
	"errors"
	"os/exec"
	"testing"
)

func TestCommandRefusesRelativeProgram(t *testing.T) {
	// "true" exists on PATH, so os/exec itself is happy; the rule is ours.
	cmd := Command("true")
	if !errors.Is(cmd.Err, ErrNotAbsolute) {
		t.Fatalf("Err = %v, want ErrNotAbsolute", cmd.Err)
	}
	if err := cmd.Run(); !errors.Is(err, ErrNotAbsolute) {
		t.Fatalf("Run = %v, want ErrNotAbsolute", err)
	}
}

func TestCommandRefusesShells(t *testing.T) {
	for _, prog := range []string{"/bin/sh", "/bin/bash", "/usr/bin/env", "/bin/ZSH"} {
		cmd := Command(prog, "-c", "echo hi")
		if !errors.Is(cmd.Err, ErrShell) {
			t.Errorf("%s: Err = %v, want ErrShell", prog, cmd.Err)
		}
		if err := cmd.Start(); !errors.Is(err, ErrShell) {
			t.Errorf("%s: Start = %v, want ErrShell", prog, err)
		}
	}
}

func TestCommandRunsAbsoluteProgram(t *testing.T) {
	cmd := Command("/bin/echo", "hi")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Output: %v", err)
	}
	if string(out) != "hi\n" {
		t.Fatalf("out = %q, want %q", out, "hi\n")
	}
}

func TestCommandContextKeepsTheDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := CommandContext(ctx, "/bin/echo", "hi").Run(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v, want context.Canceled", err)
	}
}

func TestLookPathFailureIsKept(t *testing.T) {
	cmd := Command("jorbique-vpn-no-such-program")
	if cmd.Err == nil || errors.Is(cmd.Err, ErrNotAbsolute) {
		t.Fatalf("Err = %v, want the os/exec LookPath error", cmd.Err)
	}
	var notFound *exec.Error
	if !errors.As(cmd.Err, &notFound) {
		t.Fatalf("Err = %v, want *exec.Error", cmd.Err)
	}
}
