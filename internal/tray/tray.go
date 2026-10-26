package tray

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"jorbiquevpn/internal/constants"
	"jorbiquevpn/internal/core"

	"github.com/getlantern/systray"
)

var (
	dataDir       string
	exeDir        string
	openUIItem    *systray.MenuItem
	statusItem    *systray.MenuItem
	startStopItem *systray.MenuItem
	onStop        func()
	onOpenUI      func()
	onConnect     func()
	onQuit        func()
	running       bool
	mu            sync.Mutex

	actionBusy bool
	actionMu   sync.Mutex
)

func SetDataDir(dir string) {
	dataDir = dir
}

func SetExeDir(dir string) {
	exeDir = dir
}

func SetCallbacks(stop, openUI, connect, quit func()) {
	onStop = stop
	onOpenUI = openUI
	onConnect = connect
	onQuit = quit
}

func SetRunning(r bool) {
	mu.Lock()
	running = r
	mu.Unlock()
	updateMenuItems()
}

func IsRunning() bool {
	mu.Lock()
	defer mu.Unlock()
	return running
}

func Run() {
	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetIcon(trayIcon())
	systray.SetTitle(constants.AppDisplayName + " — отключено")
	systray.SetTooltip(constants.AppDisplayName)

	openUIItem = systray.AddMenuItem("Открыть окно", "Открыть главное окно")
	systray.AddSeparator()

	statusItem = systray.AddMenuItem("Состояние: отключено", "Текущее состояние")
	statusItem.Disable()
	systray.AddSeparator()

	startStopItem = systray.AddMenuItem("Подключить", "Подключить VPN")
	systray.AddSeparator()

	quitItem := systray.AddMenuItem("Выход", "Закрыть приложение")

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		for range ticker.C {
			SetRunning(core.IsRunning())
		}
	}()

	go handleMenuClicks(quitItem)

	updateMenuItems()
}

func handleMenuClicks(quitItem *systray.MenuItem) {
	for {
		select {
		case <-openUIItem.ClickedCh:
			if onOpenUI != nil {
				onOpenUI()
			}
		case <-startStopItem.ClickedCh:
			handleStartStop()
		case <-quitItem.ClickedCh:
			if IsRunning() && onStop != nil {
				onStop()
			}
			if onQuit != nil {
				onQuit()
			}
			systray.Quit()
			return
		}
	}
}

func trayIcon() []byte {
	for _, base := range []string{exeDir, dataDir} {
		if base == "" {
			continue
		}
		for _, name := range []string{constants.AppIconICO} {
			iconPath := filepath.Join(base, name)
			if data, err := os.ReadFile(iconPath); err == nil && len(data) > 0 {
				return data
			}
		}
	}
	if len(embeddedIcon) > 0 {
		return embeddedIcon
	}
	return getDefaultIcon()
}

func onExit() {
	// systray requires an exit callback; quitting is handled where Quit is clicked.
}

// handleStartStop runs Connect/Disconnect off the systray event-loop goroutine so a ~10s connect
// never freezes Open UI / Exit / Disconnect, and a re-entrancy guard drops overlapping clicks so a
// rapid double-click cannot launch two concurrent flows.
func handleStartStop() {
	if !beginAction() {
		return
	}
	go func() {
		defer endAction()
		if IsRunning() {
			if onStop != nil {
				onStop()
			}
			SetRunning(false)
		} else if onConnect != nil {
			onConnect()
		} else if onOpenUI != nil {
			onOpenUI()
		}
	}()
}

func beginAction() bool {
	actionMu.Lock()
	defer actionMu.Unlock()
	if actionBusy {
		return false
	}
	actionBusy = true
	return true
}

func endAction() {
	actionMu.Lock()
	actionBusy = false
	actionMu.Unlock()
}

func updateMenuItems() {
	mu.Lock()
	r := running
	mu.Unlock()
	if statusItem != nil {
		if r {
			statusItem.SetTitle("Состояние: подключено")
		} else {
			statusItem.SetTitle("Состояние: отключено")
		}
	}
	if startStopItem != nil {
		if r {
			startStopItem.SetTitle("Отключить")
		} else {
			startStopItem.SetTitle("Подключить")
		}
	}
}

// getDefaultIcon returns a minimal 16x16 green ICO when no external icon exists
func getDefaultIcon() []byte {
	// Minimal valid 16x16 32bpp ICO: header(6) + dir(16) + bmp(40) + pixels(1024) + mask(32) = 1118 bytes
	h := []byte{
		0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x10, 0x10, 0x00, 0x00, 0x00, 0x00, 0x20, 0x00, 0x48, 0x04,
		0x00, 0x00, 0x16, 0x00, 0x00, 0x00, 0x28, 0x00, 0x00, 0x00, 0x10, 0x00, 0x00, 0x00, 0x20, 0x00,
		0x00, 0x00, 0x01, 0x00, 0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
	pix := make([]byte, 1024)
	for i := 0; i < 1024; i += 4 {
		pix[i] = 0x00
		pix[i+1] = 0x80
		pix[i+2] = 0x00
		pix[i+3] = 0xFF
	}
	mask := make([]byte, 32)
	return append(append(h, pix...), mask...)
}
