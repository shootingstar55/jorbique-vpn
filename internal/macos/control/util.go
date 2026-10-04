package control

import (
	"errors"
	"io"
	"os/exec"
	"strings"
)

func stringReader(s string) io.Reader { return strings.NewReader(s) }

func asExit(err error, target **exec.ExitError) bool { return errors.As(err, target) }
