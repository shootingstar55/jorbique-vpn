//go:generate go run github.com/tc-hib/go-winres@v0.3.3 make --in ../../winres --out .
package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	stdruntime "runtime"
	"strings"
	"time"

	"jorbiquevpn/frontend"
	"jorbiquevpn/internal/app"
	"jorbiquevpn/internal/constants"
	"jorbiquevpn/internal/core"
	"jorbiquevpn/internal/defender"
	"jorbiquevpn/internal/paths"
	"jorbiquevpn/internal/singleinstance"
	"jorbiquevpn/internal/tray"
	"jorbiquevpn/internal/ui"
	"jorbiquevpn/internal/winicon"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			writeStartupError(fmt.Sprintf("panic: %v", r), false)
		}
	}()

	logonAutostart := isLogonAutostartArg(os.Args[1:])
	if logonAutostart {
		appendLogonTrace("process started (logon autostart)")
	}
	ensureAdministrator(logonAutostart)

	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)
	dataDir := paths.DataDir()
	// New binary version: clear %APPDATA%/JorbiqueVPN and %TEMP%/JorbiqueVPN if the install
	// marker is missing or outdated. The user's own settings are kept — see the function.
	paths.ResetDataDirsIfVersionMismatch(constants.AppVersion)
	if err := paths.EnsureDataDirs(); err != nil {
		writeStartupError("Failed to create app data directories: "+err.Error(), false)
		os.Exit(1)
	}

	if err := paths.WriteInstallVersion(constants.AppVersion); err != nil {
		writeStartupError("Failed to write install version: "+err.Error(), false)
		os.Exit(1)
	}

	// Defender exclusions may take noticeable time (PowerShell/Defender cmdlets),
	// so do them asynchronously after basic startup checks pass.
	go defender.AddExclusions(exeDir)
	go defender.AddExclusions(dataDir)

	releaseMutex, ok := singleinstance.TryMutex()
	if !ok {
		showErrorBox("Another instance of " + constants.AppDisplayName + " is already running (only one instance is allowed).")
		os.Exit(0)
	}
	defer releaseMutex()

	backend := app.New()
	backend.StartTelemetry()
	uiApp := ui.New(backend)

	startRecoveryLoops(backend)

	doStop := func() {
		_, _ = backend.Disconnect()
		tray.SetRunning(false)
	}

	tray.SetDataDir(dataDir)
	tray.SetExeDir(exeDir)
	tray.SetRunning(backend.IsConnected())

	// Wails expects assets at root - frontend.Dist has "dist" prefix, strip it
	distFS, err := fs.Sub(frontend.Dist, "dist")
	if err != nil {
		writeStartupError("Frontend assets: "+err.Error(), false)
		os.Exit(1)
	}
	err = wails.Run(&options.App{
		Title:  constants.AppDisplayName,
		Width:  windowWidth,
		Height: windowHeight,
		// WebView2 disables the host default context menu in release builds unless this is set.
		EnableDefaultContextMenu: true,
		Windows: &windows.Options{
			WebviewUserDataPath: filepath.Join(dataDir, "webview"),
		},
		AssetServer: &assetserver.Options{
			Assets: distFS,
		},
		BackgroundColour: &options.RGBA{R: 15, G: 23, B: 42, A: 255},
		OnBeforeClose: func(ctx context.Context) (prevent bool) {
			runtime.WindowHide(ctx)
			return true
		},
		OnStartup: func(ctx context.Context) {
			uiApp.SetWailsContext(ctx)
			fitWindowToScreen(ctx)
			winicon.ApplyTitleBarFromExeDir(exeDir)
			_ = tray.RefreshAutostartIfEnabled()
			showUI := func() {
				runtime.WindowShow(ctx)
			}
			trayConnect := func() {
				openUI, err := backend.ConnectSaved()
				if openUI {
					showUI()
					return
				}
				if err != nil {
					showUI()
					return
				}
				tray.SetRunning(backend.IsConnected())
			}
			tray.SetCallbacks(doStop, showUI, trayConnect, func() {
				backend.CleanupSingboxCacheOnExit()
				releaseMutex()
				os.Exit(0)
			})
			go func() {
				stdruntime.LockOSThread()
				defer stdruntime.UnlockOSThread()
				tray.Run()
			}()
		},
		Bind: []interface{}{
			uiApp,
		},
	})

	if err != nil {
		writeStartupError(err.Error(), false)
		os.Exit(1)
	}
}

func ensureAdministrator(logonAutostart bool) {
	// Elevate before data-dir work so logon launches are not blocked by early startup steps.
	if !isAdmin() {
		if logonAutostart {
			appendLogonTrace("waiting for interactive shell")
			time.Sleep(5 * time.Second)
		}
		if err := relaunchElevated(); err != nil {
			appendLogonTrace("elevation failed: " + err.Error())
			writeStartupError("Elevation failed: "+err.Error(), logonAutostart)
			if !logonAutostart {
				showAdminMessageBox()
			}
			os.Exit(1)
		}
		if logonAutostart {
			appendLogonTrace("elevation requested, exiting bootstrap instance")
		}
		os.Exit(0)
	}
}

func startRecoveryLoops(backend *app.App) {
	// If the previous run left routes/TUN state (crash, kill, reboot), clean up before the user connects.
	go func() {
		time.Sleep(400 * time.Millisecond)
		_, _ = backend.RecoverFromUnexpectedExit()
	}()
	// If sing-box exits while the app stays open, restore routing like a normal disconnect.
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		wasRunning := core.IsRunning()
		for range ticker.C {
			running := core.IsRunning()
			if wasRunning && !running {
				_, _ = backend.RecoverFromUnexpectedExit()
			}
			wasRunning = running
		}
	}()
}

func appendLogonTrace(msg string) {
	// Write only to %APPDATA%/JorbiqueVPN, never next to the binary. This runs before
	// EnsureDataDirs, so make sure the app data folder exists first.
	dataDir := paths.DataDir()
	if dataDir == "" {
		return
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return
	}
	line := fmt.Sprintf("[%s] %s\n", time.Now().Format("2006-01-02 15:04:05"), msg)
	f, err := os.OpenFile(filepath.Join(dataDir, "logon_autostart.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	_, _ = f.WriteString(line)
	_ = f.Close()
}

func writeStartupError(msg string, quietUI bool) {
	b := []byte(msg)
	const mode os.FileMode = 0644
	writeLog := func(path string) {
		if err := os.WriteFile(path, b, mode); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "startup_error log %s: %v\n", path, err)
		}
	}
	if exe, err := os.Executable(); err == nil {
		writeLog(filepath.Join(filepath.Dir(exe), "startup_error.log"))
	}
	if dataDir := paths.DataDir(); dataDir != "" {
		writeLog(filepath.Join(dataDir, "startup_error.log"))
	}
	if !quietUI {
		showErrorBox("Startup error. Details: startup_error.log (next to the exe or in the app data folder).")
	}
}

func isLogonAutostartArg(args []string) bool {
	for _, arg := range args {
		if strings.EqualFold(strings.TrimSpace(arg), "--logon-autostart") {
			return true
		}
	}
	return false
}

// The size the window opens at, in logical pixels.
const (
	windowWidth = 754 // 580 * 1.3
	// The screen is 634px of content with the IP card empty and roughly 710
	// with a lease in it; 820 clears that with room for an IPv6 address, which
	// wraps and takes a line more. It used to be 960, which is what pushed the
	// window past the bottom of a display scaled to 125% — where the screen is
	// 864 logical pixels tall and fitWindowToScreen brings the window to 768,
	// still above the content.
	windowHeight = 820
)

// fitWindowToScreen keeps the window inside the screen it opens on, and centres
// it there. Wails reports the screen in the same logical pixels the size above
// is written in, so a display scaled to 125% is compared like any other — and
// that is the one that broke: 960 logical pixels became 1200 physical ones, the
// title bar opened above the top edge, and a title bar off-screen cannot be
// grabbed, minimised or resized.
func fitWindowToScreen(ctx context.Context) {
	screens, err := runtime.ScreenGetAll(ctx)
	if err != nil || len(screens) == 0 {
		return
	}
	screen := screens[0]
	for _, s := range screens {
		if s.IsCurrent {
			screen = s
			break
		}
	}
	w, h := ui.FitWindowToScreen(windowWidth, windowHeight, screen.Size.Width, screen.Size.Height)
	if w != windowWidth || h != windowHeight {
		runtime.WindowSetSize(ctx, w, h)
	}
	runtime.WindowCenter(ctx)
}
