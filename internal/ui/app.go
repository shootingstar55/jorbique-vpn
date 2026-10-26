package ui

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"jorbiquevpn/internal/app"
	"jorbiquevpn/internal/autoconnect"
	"jorbiquevpn/internal/dashboard"
	"jorbiquevpn/internal/ipinfo"
	"jorbiquevpn/internal/utils"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the Wails-bound application; frontend calls these methods by name.
type App struct {
	backend *app.App

	ctxMu    sync.RWMutex
	wailsCtx context.Context // from OnStartup; required for runtime.OpenFileDialog
}

// SetWailsContext stores the lifecycle context from main (OnStartup). Not for use from JS.
func (a *App) SetWailsContext(ctx context.Context) {
	a.ctxMu.Lock()
	defer a.ctxMu.Unlock()
	a.wailsCtx = ctx
}

func New(backend *app.App) *App {
	return &App{backend: backend}
}

func (a *App) IsConnected() bool {
	return a.backend.IsConnected()
}

// GetIPInfo backs the IP-information card.
func (a *App) GetIPInfo(force bool) (ipinfo.Info, error) {
	return a.backend.GetIPInfo(force)
}

func (a *App) IsAutostartEnabled() bool {
	return a.backend.IsAutostartEnabled()
}

func (a *App) SetAutostartEnabled(on bool) error {
	return a.backend.SetAutostartEnabled(on)
}

func (a *App) Connect() error {
	return wrapBackendError(a.backend.Connect())
}

// CancelConnect stops a connection check in progress; the pending Connect fails with "Connection cancelled".
func (a *App) CancelConnect() { a.backend.CancelConnect() }

func (a *App) Disconnect() error {
	return wrapBackendError(a.backend.Disconnect())
}

// HasUserUUID tells the window whether to show the first-run UUID screen instead of the app.
func (a *App) HasUserUUID() bool {
	return a.backend.HasUserUUID()
}

// ActivateUserUUID stores the UUID from the first-run screen once the server knows it.
func (a *App) ActivateUserUUID(userUUID string) error {
	return wrapBackendError(a.backend.ActivateUserUUID(userUUID))
}

// SetVpnUserConfig saves user UUID, outbound toggles, and optional direct-domain hostnames (one per line).
func (a *App) SetVpnUserConfig(userUUID string, outboundEnabled map[string]bool) error {
	return wrapBackendError(a.backend.SetVpnUserConfig(userUUID, outboundEnabled))
}

func (a *App) GetUserUUID() string { return a.backend.GetUserUUID() }

func (a *App) GetConnectionProgress() autoconnect.Progress { return a.backend.GetConnectionProgress() }

func (a *App) SetUserUUID(uuid string) error { return wrapBackendError(a.backend.SetUserUUID(uuid)) }

// SetNaiveMode switches between the NaiveProxy mode and the profile's other protocols.
func (a *App) SetNaiveMode(on bool) error {
	return wrapBackendError(a.backend.SetNaiveMode(on))
}

// GetDirectDomains returns the extra direct domains, one per line.
func (a *App) GetDirectDomains() string {
	return a.backend.GetDirectDomains()
}

// SetDirectDomains saves the extra direct domains.
func (a *App) SetDirectDomains(block string) error {
	return wrapBackendError(a.backend.SetDirectDomains(block))
}

func wrapBackendError(msg string, err error) error {
	if err != nil {
		if msg != "" {
			return fmt.Errorf("%s: %w", msg, err)
		}
		return err
	}
	return nil
}

func (a *App) GetLogContent(logName string) (string, error) {
	return a.backend.GetLogContent(logName)
}

func (a *App) ListLogFiles() ([]string, error) {
	return a.backend.ListLogFiles()
}

func (a *App) ClearLogs() error {
	return a.backend.ClearLogs()
}

func (a *App) GetVpnUserConfig() app.VpnUserConfigForm {
	return a.backend.GetVpnUserConfig()
}

// SaveVpnFormDraft persists the VPN modal fields without validation (debounced autosave from UI).
func (a *App) SaveVpnFormDraft(uuid string, directDomains string, outboundEnabled map[string]bool) error {
	return a.backend.SaveVpnFormDraft(uuid, directDomains, outboundEnabled)
}

func (a *App) HasVpnUserConfig() bool {
	return a.backend.HasVpnUserConfig()
}

// GetAvailableOutbounds lists what the downloaded sing-box profile offers, so
// the settings window can draw a row per protocol instead of a list written
// into the page.
func (a *App) GetAvailableOutbounds() []string {
	return a.backend.GetAvailableOutbounds()
}

func (a *App) IsSingboxInstalled() bool {
	return a.backend.IsSingboxInstalled()
}

func (a *App) GetSingboxLocalVersion() string {
	return a.backend.GetSingboxLocalVersion()
}

func (a *App) IsSingboxUpdateAvailable() bool {
	return a.backend.IsSingboxUpdateAvailable()
}

func (a *App) InstallSingbox() error {
	_, err := a.backend.InstallSingbox(a.emitSingboxProgress)
	return err
}

func (a *App) UpdateSingbox() error {
	_, err := a.backend.UpdateSingbox(a.emitSingboxProgress)
	return err
}

// emitSingboxProgress forwards download percent (0..100) to the frontend as a
// "singbox-progress" Wails event so it can drive a progress bar.
func (a *App) emitSingboxProgress(pct int) {
	if a.wailsCtx != nil {
		runtime.EventsEmit(a.wailsCtx, "singbox-progress", pct)
	}
}

func (a *App) ListActiveApps() ([]utils.ActiveProcess, error) {
	return a.backend.ListActiveApps()
}

func (a *App) GetIgnoredProcessNames() []string {
	return a.backend.GetIgnoredProcessNames()
}

func (a *App) GetIgnoredProcessPaths() []string {
	return a.backend.GetIgnoredProcessPaths()
}

func (a *App) SetIgnoredProcessNamesAndPaths(names []string, paths []string) error {
	return a.backend.SetIgnoredProcessNamesAndPaths(names, paths)
}

func (a *App) DashboardSnapshot() (dashboard.Snapshot, error) {
	return a.backend.DashboardSnapshot(context.Background())
}

func (a *App) DashboardSelectOutbound(group, outbound string) error {
	return a.backend.DashboardSelectOutbound(context.Background(), group, outbound)
}

func (a *App) DashboardURLTest(outbound string) error {
	return a.backend.DashboardURLTest(context.Background(), outbound)
}

// PickExecutableForBypass opens a file dialog to choose an .exe; returns empty string if cancelled.
func (a *App) PickExecutableForBypass() (string, error) {
	a.ctxMu.RLock()
	ctx := a.wailsCtx
	a.ctxMu.RUnlock()
	if ctx == nil {
		return "", errors.New("application context not ready")
	}
	return runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{
		Title: "Select executable",
		Filters: []runtime.FileFilter{
			{DisplayName: "Executable (*.exe)", Pattern: "*.exe"},
			{DisplayName: "All files", Pattern: "*.*"},
		},
	})
}

// windowTitleBarHeight is the chrome WindowSetSize counts but the page cannot
// measure: the title bar and the border around it.
const windowTitleBarHeight = 40

// FitWindowToContent sizes the window to the height the page reports, bounded by
// the screen it is on.
//
// Guessing that height from here has been wrong three times over: the layout is
// measured in one place and rendered in another, where the display scaling and
// the system text size both stretch it. The page knows what it actually
// occupies, so it says so, and the window follows — with the screen still the
// ceiling, because a window taller than the desktop cannot be moved or closed.
func (a *App) FitWindowToContent(contentHeight int) {
	a.ctxMu.RLock()
	ctx := a.wailsCtx
	a.ctxMu.RUnlock()
	if ctx == nil || contentHeight <= 0 {
		return
	}
	width, height := runtime.WindowGetSize(ctx)
	screenWidth, screenHeight := 0, 0
	if screens, err := runtime.ScreenGetAll(ctx); err == nil {
		for _, s := range screens {
			if s.IsCurrent {
				screenWidth, screenHeight = s.Size.Width, s.Size.Height
				break
			}
		}
	}
	wanted, change := windowHeightForContent(contentHeight, height, width, screenWidth, screenHeight)
	if !change {
		return
	}
	runtime.WindowSetSize(ctx, width, wanted)
}

// windowHeightResizeThreshold is how far off the window has to be before it is
// worth moving: a resize the user can see, for a pixel they cannot, is worse
// than the pixel.
const windowHeightResizeThreshold = 8

// windowHeightForContent is the arithmetic behind FitWindowToContent: the height
// to set, and whether it is worth setting.
func windowHeightForContent(contentHeight, currentHeight, width, screenWidth, screenHeight int) (int, bool) {
	if contentHeight <= 0 {
		return currentHeight, false
	}
	wanted := contentHeight + windowTitleBarHeight
	_, wanted = FitWindowToScreen(width, wanted, screenWidth, screenHeight)
	if diff := wanted - currentHeight; diff > -windowHeightResizeThreshold && diff < windowHeightResizeThreshold {
		return currentHeight, false
	}
	return wanted, true
}
