package ui

import (
	"context"
	"jorbiquevpn/internal/clientupdate"
	"os"
	"runtime"
	"time"
)

func (a *App) CheckClientUpdate() (clientupdate.Status, error) {
	return clientupdate.Check(context.Background())
}

func (a *App) InstallClientUpdate() error {
	err := clientupdate.Install(context.Background(), func() error {
		_, err := a.backend.Disconnect()
		return err
	})
	if err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		a.backend.CleanupSingboxCacheOnExit()
		// Let Wails deliver the answer before closing; the native updater waits for
		// process termination, which also releases the Windows single-instance mutex.
		go func() { time.Sleep(500 * time.Millisecond); os.Exit(0) }()
	}
	return nil
}
