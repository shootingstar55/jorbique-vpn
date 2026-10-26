//go:build darwin

package ui

import (
	"strings"

	"jorbiquevpn/internal/deeplink"
	"jorbiquevpn/internal/macos/applekit"
)

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
	if c.gateP == nil {
		cfg, _ := c.loadConfig()
		if strings.EqualFold(singboxUUID(cfg), uuid) {
			c.cmdOpenMainWindow()
			return
		}
	}
	safeGo(func() {
		res := c.run.runWithStdin(c.exePath, []string{setSingboxConfigCommand}, uuid)
		applekit.RunOnMain(func() {
			if res.status != 0 {
				applekit.PresentAlert("Не удалось сохранить UUID", importLinkError(uuidGateError(res.stderr)), applekit.AlertWarning)
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
	applekit.PresentAlert("UUID сохранён", "Ссылка со страницы подписки сработала. Можно подключаться.", applekit.AlertInformational)
}

// importLinkError puts the daemon's refusal in the words the first-run window
// uses (frontend/src/lib/activation.js).
func importLinkError(reason string) string {
	text := strings.ToLower(reason)
	switch {
	case strings.Contains(text, "not registered") || strings.Contains(text, "unknown user uuid"):
		return "Сервер не знает этот UUID. Проверьте ссылку на странице подписки."
	case strings.Contains(text, "reach the server") || strings.Contains(text, "unreachable"):
		return "Не удалось связаться с сервером. Проверьте подключение к интернету и откройте ссылку ещё раз."
	case strings.Contains(text, "daemon") || strings.Contains(text, "service is not running"):
		return "Служба Jorbique VPN Extended не запущена. Переустановите приложение и попробуйте ещё раз."
	}
	return reason
}
