package main

import (
	"os"
	"runtime"

	"jorbiquevpn/internal/macos/cli"
	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/macos/ui"
)

func main() {
	// AppKit must be initialized and run from the process main OS thread.
	runtime.LockOSThread()

	cli.SetMenuBarRunner(ui.RunStatusBarApp)
	if err := cli.Run(); err != nil {
		cli.ReportError(err)
		os.Exit(core.ExitCodeFor(err))
	}
}
