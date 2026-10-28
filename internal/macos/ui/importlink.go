//go:build darwin

package ui

import (
	"context"
	"errors"
	"strings"
	"time"

	"jorbiquevpn/internal/account"
	"jorbiquevpn/internal/deeplink"
	"jorbiquevpn/internal/macos/applekit"
	"jorbiquevpn/internal/macos/core"
)

// launchLinkWait bounds how long a copy that is about to exit waits for the
// link it may have been opened with. The URL arrives right after AppKit checks
// in, so a link launch returns well before it; only a plain launch waits it out.
const launchLinkWait = 1500 * time.Millisecond

// ForwardLaunchLink is for a menu-bar launch that hands over to another copy
// and exits — another bar already runs, or the login agent is started in its
// place. macOS gives a jorbique:// link to the process it launched, so this
// copy takes it and leaves the UUID in the user's run folder, where the bar
// that stays picks it up (watchPendingLinks). Main thread, before any other
// AppKit use.
func ForwardLaunchLink() {
	uuid, ok := deeplink.FromArgs(applekit.CollectLaunchURLs(launchLinkWait))
	if !ok {
		return
	}
	_ = deeplink.SavePending(core.NewPaths().RunDir, uuid)
}

// pendingLinkPoll is how often the bar looks for a link another copy handed
// over: one stat of a file that is almost never there.
const pendingLinkPoll = time.Second

// watchPendingLinks takes the links ForwardLaunchLink leaves behind.
func (c *statusBarController) watchPendingLinks() {
	runDir := core.NewPaths().RunDir
	safeGo(func() {
		for range time.Tick(pendingLinkPoll) {
			if !deeplink.HasPending(runDir) {
				continue
			}
			applekit.RunOnMain(func() {
				if uuid := deeplink.TakePending(runDir); uuid != "" {
					c.openLinks([]string{deeplink.Scheme + "://import?uuid=" + uuid})
				}
			})
		}
	})
}

// openLinks answers a jorbique:// link (the /sub page's «Открыть в Jorbique
// VPN»): the UUID in it is saved the way the first-run window saves one —
// through the daemon, which asks the server first — and the app is then ready
// to connect. Runs on the main thread; the save itself does not.
func (c *statusBarController) openLinks(urls []string) {
	uuid, ok := deeplink.FromArgs(urls)
	if !ok {
		applekit.PresentAlert("Ссылка не подошла",
			"В ссылке нет UUID пользователя. Откройте её ещё раз со страницы подписки.", applekit.AlertWarning)
		return
	}
	needsSave := true
	if c.gateP == nil {
		cfg, _ := c.loadConfig()
		needsSave = !strings.EqualFold(singboxUUID(cfg), uuid)
	}
	safeGo(func() {
		err := account.Import(context.Background(), uuid, func() error {
			if !needsSave {
				return nil
			}
			res := c.run.runWithStdin(c.exePath, []string{setSingboxConfigCommand}, uuid)
			if res.status != 0 {
				return errors.New(importLinkError(uuidGateError(res.stderr)))
			}
			return nil
		})
		applekit.RunOnMain(func() {
			if err != nil {
				applekit.PresentAlert("Не удалось импортировать UUID", importLinkError(err.Error()), applekit.AlertWarning)
				return
			}
			if !needsSave {
				c.cmdOpenMainWindow()
				return
			}
			c.importAccepted()
		})
	})
}

// importAccepted carries on after a linked UUID is stored: a first run starts
// the app proper and closes the first-run window, a running app reconnects an
// open tunnel so it uses the new UUID. Either way the main window comes up.
func (c *statusBarController) importAccepted() {
	if c.gateP != nil {
		c.gateAccepted = true
		c.finishStartup()
		c.gateP.Close()
	} else {
		cfg, paths := c.loadConfig()
		if running := startedVPNs(cfg, paths); len(running) > 0 {
			c.runRestart(running, append([]string{"restart"}, running...))
		} else {
			c.refresh()
		}
	}
	c.cmdOpenMainWindow()
	applekit.PresentAlert("UUID сохранён", "Это устройство привязано к подписке. Можно подключаться.", applekit.AlertInformational)
}

// importLinkError puts the daemon's refusal in the words the first-run window
// uses (frontend/src/lib/activation.js).
func importLinkError(reason string) string {
	text := strings.ToLower(reason)
	switch {
	case strings.Contains(text, "лимит устройств"):
		return "Достигнут лимит устройств. Отвяжите ненужное устройство на странице подписки и откройте ссылку ещё раз."
	case strings.Contains(text, "not registered") || strings.Contains(text, "unknown user uuid"):
		return "Такого пользователя не существует. Проверьте ссылку на странице подписки."
	case strings.Contains(text, "reach the server") || strings.Contains(text, "unreachable"):
		return "Не удалось связаться с сервером. Проверьте подключение к интернету и откройте ссылку ещё раз."
	case strings.Contains(text, "daemon") || strings.Contains(text, "service is not running"):
		return "Служба Jorbique VPN Extended не запущена. Переустановите приложение и попробуйте ещё раз."
	}
	return reason
}
