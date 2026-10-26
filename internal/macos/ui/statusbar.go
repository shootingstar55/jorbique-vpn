//go:build darwin

package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"jorbiquevpn/internal/autoconnect"
	"jorbiquevpn/internal/ipinfo"
	"jorbiquevpn/internal/macos/applekit"
	"jorbiquevpn/internal/macos/core"
	"jorbiquevpn/internal/update"
)

const setSingboxConfigCommand = "set-singbox-config"

// statusBarController drives the menu bar: the status item and its glyph, the
// periodic status refresh, connection actions, and the main window.
//
// Threading: the menu is built once on the main thread; status refreshes run on
// a background goroutine and marshal their UI updates back via
// applekit.RunOnMain. Blocking subprocesses (update-singbox, quit's stop-all)
// likewise run off the main thread so the menu never hangs.
type statusBarController struct {
	exePath     string
	displayName string
	run         *runner

	mu         sync.Mutex
	lastStart  time.Time // zero when no recent start
	lastStop   time.Time // zero when no recent stop
	cachedIP   string    // formatted "ip (cc)" exit IP, "" when unknown
	ipFetching bool      // an exit-ip fetch goroutine is in flight
	ipGen      int       // bumped by invalidateIPInfo; a lookup from an older generation is dropped
	spinning   bool      // a spinner goroutine is animating an action

	// connFail decides when a failing check is shown in the menu bar. Outside
	// the mutex above, because only the main thread touches it (applyRefresh).
	connFail connFailTracker
	// lastProfileFetch is when the profile was last downloaded, so a cached one
	// is refreshed on a schedule rather than on every pass; zero when never.
	lastProfileFetch time.Time

	// restart, starting and stopping hold the actions in flight; while any is
	// busy the status glyph animates instead of showing steady state
	// (actionprogress.go).
	restart  opTracker
	starting opTracker
	stopping opTracker
	quitting opTracker

	singboxP       *webPanel // the open sing-box config panel, if any
	routeP         *webPanel // the open route-rules panel, if any
	mainP          *webPanel // the main window, if open
	gateP          *webPanel // the first-run UUID window, while no UUID is stored
	gateAccepted   bool      // the gate's UUID was saved; closing it no longer quits
	lastRunningKey string    // invalidates exit-IP details when connection changes
	menuConnected  bool      // main-thread connection state for the menu action

	app        applekit.App
	statusItem applekit.StatusItem
	barIcons   map[string]applekit.Image // the face per status color keyword, built on first use

	headerItem  applekit.MenuItem
	detailItem  applekit.MenuItem
	connectItem applekit.MenuItem
	restartItem applekit.MenuItem
	quitItem    applekit.MenuItem

	// ipCache backs the window's IP-information card, guarded by mu.
	ipCache *ipinfo.Cache

	// profileFetching guards the profile download, which the refresh loop
	// retries while the cache is missing. Guarded by mu.
	profileFetching bool
}

// RunStatusBarAppPurego is the entry point for the menu-bar app. exePath is the
// CLI it drives; displayName is the user-visible app name. It blocks in the
// AppKit run loop until the user quits. Config is read relative to the running
// binary (core.NewPaths), so for a faithful menu it must run as the installed
// CLI.
func RunStatusBarAppPurego(exePath, displayName string) error {
	c := &statusBarController{exePath: exePath, displayName: displayName, run: newRunner()}
	// Resolve the GUI login user via SCDynamicStore so root-context
	// subprocesses run in the right account.
	c.run.consoleUserFromStore = applekit.ConsoleUser

	app, err := applekit.NewApp()
	if err != nil {
		return err
	}
	c.app = app
	// The Dock icon opens the window, like every other app's does. The menu
	// item does the same thing; this is the same command on the other handle.
	app.OnReopen(c.reopen)
	app.InstallEditMenu()
	// Use the installed app icon for alerts/Dock (staged next to the binary).
	applekit.SetAppIcon(filepath.Join(filepath.Dir(exePath), "AppIcon.icns"))

	// No UUID, no app: the metrics are counted by it, so until the server has
	// accepted one the first-run window is all there is — no menu, no loops.
	// A config that cannot be read is not "no UUID": that user cannot use the
	// client at all, and a UUID window would not help them.
	if cfg, err := core.ReadConfig(core.NewPaths()); err == nil && strings.TrimSpace(cfg.Singbox.UUID) == "" {
		if err := c.showUUIDGate(); err != nil {
			applekit.PresentAlert("Не удалось открыть "+displayName, err.Error(), applekit.AlertWarning)
			return err
		}
		app.Run()
		return nil
	}
	c.finishStartup()
	app.Run()
	return nil
}

// finishStartup is the app proper: the status item, its menu and the loops.
func (c *statusBarController) finishStartup() {
	c.statusItem = applekit.InstallStatusItem()
	c.statusItem.SetToolTip(c.displayName)
	c.setBarIcon("stop", 1)
	c.statusItem.SetMenu(c.buildMenu())

	c.refresh()
	// Keep sing-box in sync with the server build: a check now and then on a
	// schedule for as long as the app is open (see singboxupdate.go). No channel
	// choice and no version UI are shown to the user.
	c.startSingboxUpdateLoop()
	// And the sing-box profile, which the panels read to know what the server
	// offers. Without this the menu bar only learned about it by connecting, so
	// a fresh install opened VPN Config on an empty list.
	c.ensureRemoteProfile()
	c.startRefreshLoop()
	c.openWindowIfIconHidden()
}

// hiddenIconCheckDelay gives the menu bar time to lay the new item out before
// asking whether it made it on screen.
const hiddenIconCheckDelay = 2 * time.Second

// openWindowIfIconHidden opens the main window when the status item did not
// fit in the menu bar. On a notched MacBook with many status items (another
// VPN client's among them) macOS silently drops the ones that do not fit, and
// the app looked as if it had not started at all. The window has the same
// controls as the menu; Cmd-dragging the icon to the right of the others keeps
// it in view from then on.
func (c *statusBarController) openWindowIfIconHidden() {
	time.AfterFunc(hiddenIconCheckDelay, func() {
		applekit.RunOnMain(func() {
			if !c.statusItem.IsVisible() && c.mainP == nil {
				c.cmdOpenMainWindow()
			}
		})
	})
}

// showUUIDGate opens the first-run window. Saving goes the way VPN Config
// saves, through the daemon, which asks the server before it stores anything;
// the stored toggles go along so saving the UUID changes nothing else.
// Closing the window without a saved UUID quits.
func (c *statusBarController) showUUIDGate() error {
	panel, err := showUUIDGatePanel(
		func(uuid string) error {
			if !validSingboxUUID(uuid) {
				return fmt.Errorf("Invalid user UUID format")
			}
			res := c.run.runWithStdin(c.exePath, []string{setSingboxConfigCommand}, uuid)
			if res.status != 0 {
				return fmt.Errorf("%s", uuidGateError(res.stderr))
			}
			// Queued before the page's close request, so the window closes on
			// an app that has already started.
			applekit.RunOnMain(func() {
				c.gateAccepted = true
				c.finishStartup()
			})
			return nil
		},
		func() {
			c.gateP = nil
			if !c.gateAccepted {
				c.quit()
			}
		},
	)
	if err != nil {
		return err
	}
	panel.SetNormalLevel()
	c.gateP = panel
	return nil
}

// reopen answers a Dock click or a second launch: the first-run window while
// it is up, the main window after.
func (c *statusBarController) reopen() {
	if c.gateP != nil {
		c.gateP.Show()
		return
	}
	c.cmdOpenMainWindow()
}

// loadConfig reads the effective config and paths, never returning nil.
func (c *statusBarController) loadConfig() (*core.AppConfig, *core.Paths) {
	paths := core.NewPaths()
	cfg, _ := core.ReadConfig(paths)
	if cfg != nil {
		ep := cfg.EffectivePaths()
		paths = core.NewPathsWithEffective(&ep)
	} else {
		cfg = &core.AppConfig{}
	}
	return cfg, paths
}

// buildMenu keeps connection controls one click away in a native macOS menu.
func (c *statusBarController) buildMenu() applekit.Menu {
	applekit.ResetActions()
	menu := applekit.NewMenu()
	menu.SetAutoenablesItems(false)
	menu.SetMinimumWidth(260)

	brand := menu.AddTitled(c.displayName)
	brand.SetEnabled(false)
	brand.SetMutedTitle(c.displayName)
	c.headerItem = menu.AddTitled("Отключено")
	c.headerItem.SetEnabled(false)
	c.detailItem = menu.AddTitled(disconnectedDetail)
	c.detailItem.SetEnabled(false)
	c.detailItem.SetSubtitle(disconnectedDetail)
	menu.AddSeparator()

	c.connectItem = menu.AddTitled("Подключить")
	c.connectItem.SetSymbol("power")
	c.connectItem.SetAction(c.toggleConnection)
	c.restartItem = menu.AddTitled("Переподключить")
	c.restartItem.SetSymbol("arrow.clockwise")
	c.restartItem.SetEnabled(false)
	c.restartItem.SetAction(c.cmdRestart)
	menu.AddSeparator()

	openWindow := applekit.NewMenuItemKey("Открыть "+c.displayName, "o")
	openWindow.SetSymbol("macwindow")
	openWindow.SetAction(c.cmdOpenMainWindow)
	menu.AddItem(openWindow)
	menu.AddSeparator()

	c.quitItem = applekit.NewMenuItemKey("Выйти из "+c.displayName, "q")
	c.quitItem.SetSymbol("rectangle.portrait.and.arrow.right")
	c.quitItem.SetAction(c.quit)
	menu.AddItem(c.quitItem)
	menu.OnWillOpen(c.refresh)
	return menu
}

// toggleConnection uses the same sing-box connection as the main window.
func (c *statusBarController) toggleConnection() {
	action, busy := c.currentAction()
	if busy && action.verb != "Connecting" {
		return
	}
	names := c.singboxVPNNames()
	if len(names) == 0 {
		return
	}
	// A connect still checking connections stops on a stop: the start polls
	// for it and gives up before it spawns sing-box.
	if busy || c.menuConnected {
		c.runStop(names, append([]string{"stop"}, names...))
	} else {
		c.startSelected(names)
	}
}

// updateConnectionActions also runs immediately on a click, before status returns.
func (c *statusBarController) updateConnectionActions() {
	action, busy := c.currentAction()
	title := "Подключить"
	if c.menuConnected {
		title = "Отключить"
	}
	cancellable := busy && action.verb == "Connecting"
	if busy {
		title = action.label() + "…"
	}
	if cancellable {
		title = "Отмена"
	}
	c.connectItem.SetTitle(title)
	c.connectItem.SetEnabled(!busy || cancellable)
	c.restartItem.SetEnabled(c.menuConnected && !busy)
	c.quitItem.SetEnabled(!c.quitting.busy())
}

// ---------------------------------------------------------------------------
// Refresh.
// ---------------------------------------------------------------------------

// startRefreshLoop refreshes the menu every 15s. A Go ticker drives it; each
// tick's UI work is marshaled to the main thread inside refresh.
func (c *statusBarController) startRefreshLoop() {
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for range t.C {
			c.refresh()
			c.ensureRemoteProfile()
		}
	}()
}

// refresh reads `status` off the main thread, then applies the parsed result to
// the menu on the main thread. Ports the data-gathering half of refreshStatus;
// exit-IP enrichment is deferred (the plan fetches it separately).
func (c *statusBarController) refresh() {
	go func() {
		cfg, paths := c.loadConfig()
		out, rc := c.run.runExeStatusCapture(c.exePath, true)
		var running []string
		ms := parseMenuStatus("")
		if rc == 0 {
			running = parseRunningVPNs(out)
			ms = parseMenuStatus(out)
		}
		avail := availableVPNs(cfg, paths)
		active := ""
		if report, ok := autoconnect.ReadReport(paths.RunDir); ok {
			active = report.Active
		}
		applekit.RunOnMain(func() { c.applyRefresh(running, ms, avail, active) })
	}()
}

// applyRefresh updates the menu from parsed status. Runs on the main thread.
//
// active is the protocol the last connection check started the tunnel on.
func (c *statusBarController) applyRefresh(running []string, ms MenuStatus, avail []AvailableVPN, active string) {
	// A VPN counts as no longer connecting when its process is up and its own
	// check is not failing: a tunnel whose core is alive but whose probe still
	// fails is mid-handshake, and that is the one the header should keep naming.
	done := map[string]struct{}{}
	failing := map[string]struct{}{}
	for _, n := range connectivityFailures(ms) {
		failing[n] = struct{}{}
	}
	for _, n := range running {
		if _, bad := failing[n]; !bad {
			done[n] = struct{}{}
		}
	}
	c.starting.settled(done)

	_, inProgress := c.currentAction()
	c.menuConnected = hasSingboxTypeRunning(running, avail)
	c.updateConnectionActions()
	// A different tunnel is a different exit address: drop the old one before
	// asking for it, or the menu shows (and keeps) the address from before.
	if key := strings.Join(running, "\x00"); key != c.lastRunningKey {
		c.lastRunningKey = key
		c.invalidateIPInfo()
	}
	exitIP := c.exitIPForDisplay(c.menuConnected)
	confirmedFails := c.connFail.observe(connectivityFailures(ms), time.Now())
	detail := connectionDetail(ms, c.menuConnected, exitIP, confirmedFails)
	if c.menuConnected && len(confirmedFails) == 0 && active != "" {
		detail += "  ·  " + protocolLabel(active)
	}
	c.detailItem.SetSubtitle(detail)

	p := computeStatusPresentation(running, confirmedFails)
	if len(running) == 0 && !c.recentlyStarted() && !c.recentlyStopped() {
		c.clearTransients() // idle: drop stale start/stop timestamps
	}
	if inProgress {
		// The spinner owns the glyph and header until the action finishes;
		// overwriting them here would freeze the animation and flash the
		// mid-action states (still-connected, then a failing HTTP check) that
		// the spinner exists to hide.
		return
	}
	color := applekit.StatusColor(p.color)
	c.setBarIcon(barIconKeyword(p), 1)
	if p.colored {
		c.headerItem.SetColoredTitle(p.header, color)
	} else {
		c.headerItem.SetMutedTitle(p.header)
	}
}

// setBarIcon shows the face for a status color keyword ("ok"/"yellow"/"stop")
// at the given opacity. Main thread only.
func (c *statusBarController) setBarIcon(keyword string, alpha float64) {
	img, ok := c.barIcons[keyword]
	if !ok {
		data, template := barIconPNG(keyword)
		img = applekit.NewImage(data, BarIconPoints, BarIconPoints, template, c.displayName)
		if c.barIcons == nil {
			c.barIcons = map[string]applekit.Image{}
		}
		c.barIcons[keyword] = img
	}
	c.statusItem.SetImage(img, alpha)
}

// safeGo runs fn in a goroutine, recovering panics so a background handler can
// never crash the whole menu-bar process.
func safeGo(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "panic in background handler: %v\n", r)
			}
		}()
		fn()
	}()
}

// vpnAutostart runs an install/uninstall of the VPN boot LaunchDaemon (root via
// sudo -n) and refreshes once it has actually completed, so the menu reflects
// the new state. Ports cmdInstall/UninstallVPNAutostart, but waiting instead of
// fire-and-forget so the change is visible.
func (c *statusBarController) vpnAutostart(subcmd string) {
	safeGo(func() {
		c.run.runAndCaptureOut(c.exePath, []string{subcmd})
		c.refresh()
	})
}

// exitIPForDisplay returns the cached exit IP label when a sing-box VPN is
// running, starting a background lookup if it is running but unknown; it clears
// the label when no sing-box VPN is up.
func (c *statusBarController) exitIPForDisplay(sbRunning bool) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !sbRunning {
		c.cachedIP = ""
		return ""
	}
	if c.cachedIP == "" && !c.ipFetching {
		c.ipFetching = true
		gen := c.ipGen
		go c.fetchExitIP(gen)
	}
	return c.cachedIP
}

// fetchExitIP looks the exit address up through the same cache the window's IP
// card reads, so the menu and the window agree and whichever asks second gets
// the answer for free. The menu used to run `exit-ip wait` instead: a
// subprocess (via sudo or launchctl when root) polling other services until it
// saw the same address twice, which could take up to 45s or fail outright
// while the window already showed the address. A lookup that fails is retried
// by the next refresh. gen is the invalidation generation the lookup started
// in; an answer from before a connect or disconnect is dropped.
func (c *statusBarController) fetchExitIP(gen int) {
	info, err := c.ipInfo(false)
	var label string
	if err == nil && info.IP != "" {
		label = ipLabel(info.IP, info.CountryCode)
	}
	c.mu.Lock()
	c.ipFetching = false
	stale := gen != c.ipGen
	if label != "" && !stale {
		c.cachedIP = label
	}
	c.mu.Unlock()
	if label != "" && !stale {
		c.refresh()
	}
}

// autoUpdateSingboxIfNeeded keeps the installed sing-box in sync with the update
// server on launch. It asks `singbox-versions` for the local and server versions
// and installs newer releases or the channel's migration to sing-box-extended.
// Within one distribution it never downgrades. On a FIRST
// install (no local binary — the bundle is no longer shipped) it shows a progress
// window so the download is visible; a routine background upgrade stays silent. A
// matching version is a no-op, and an unreachable server leaves the binary as-is.
func (c *statusBarController) autoUpdateSingboxIfNeeded() {
	out, rc := c.run.runSubcmdCapture(c.exePath, []string{"singbox-versions"})
	if rc != 0 || out == "" {
		c.recordSingboxCheck(false)
		return
	}
	v, ok := parseSingboxVersions(out)
	if !ok {
		c.recordSingboxCheck(false)
		return
	}
	server := strings.TrimSpace(v.Remote)
	local := strings.TrimSpace(v.Local)
	if server == "" {
		// Server unreachable / no build advertised — keep what we have and try
		// again sooner than a full interval.
		c.recordSingboxCheck(false)
		return
	}
	if local == "" {
		// First launch: nothing installed, fetch from the server.
		c.recordSingboxCheck(c.downloadSingboxWithProgress("Скачиваем sing-box "+server+"…") == 0)
		return
	}
	if !update.ShouldUpdateSingbox(local, server) {
		c.recordSingboxCheck(true)
		return
	}
	// An upgrade downloads the same way a first install does. It used to run
	// silently, which meant the one thing the user could see — a several-MB
	// download over a slow link — looked like the app doing nothing at all.
	c.recordSingboxCheck(c.downloadSingboxWithProgress("Обновляем sing-box "+local+" → "+server+"…") == 0)
}

// renewFailureBody builds the alert text for a failed config sync: an
// actionable hint first when the failure is recognizable, the raw output after.
// An NSAlert grows to fit its text and does not scroll: a `renew-certs` that
// fails for five profiles prints a screenful per profile, and the window ended
// up taller than the display with its OK button below the bottom edge — an
// alert nobody could dismiss. The log keeps the whole thing.
const alertOutputLines = 12

// downloadSingboxWithProgress installs sing-box from the server while showing a
// dark progress window driven by the live "PROGRESS <pct>" lines the privileged
// update-singbox subprocess streams. UI work hops to the main thread; the blocking
// download runs in this background goroutine.
func (c *statusBarController) downloadSingboxWithProgress(message string) int {
	var pw applekit.ProgressWindow
	ready := make(chan struct{})
	applekit.RunOnMain(func() {
		pw = applekit.NewProgressWindow(c.displayName, message)
		close(ready)
	})
	<-ready

	_, status := c.run.runStreaming(c.exePath, []string{"update-singbox"}, func(line string) {
		if pct, ok := parseProgressLine(line); ok {
			applekit.RunOnMain(func() { pw.SetPercent(pct) })
		}
	})

	applekit.RunOnMain(func() {
		if status == 0 {
			pw.SetPercent(100)
			pw.SetMessage("sing-box установлен.")
		} else {
			pw.SetMessage("Не удалось скачать. Клиент попробует ещё раз позже.")
		}
	})
	time.Sleep(700 * time.Millisecond) // let the user see the final state
	applekit.RunOnMain(pw.Close)
	c.refresh()
	return status
}

// runRestart restarts while the status item spins.
func (c *statusBarController) runRestart(names []string, args []string) {
	c.markStarted()
	c.runAction(&c.restart, names, args, restartWatchdog)
}

// runStart connects while the status item spins.
func (c *statusBarController) runStart(names []string, args []string) {
	c.markStarted()
	c.runAction(&c.starting, names, args, startWatchdog)
}

// runStop disconnects while the status item spins.
func (c *statusBarController) runStop(names []string, args []string) {
	c.markStopped()
	// A restart in flight would otherwise keep its spinner (and its watchdog)
	// alive over the stop. Clearing the trackers only ends the animation — the
	// CLI subprocess behind it is not cancellable from here, so a restart far
	// enough along can still bring the tunnel back.
	// ponytail: display-level fix; cancel the subprocess if that turns out to
	// happen in practice.
	c.restart.clear()
	c.starting.clear()
	c.runAction(&c.stopping, names, args, stopWatchdog)
}

// runAction runs a menu action's subcommand while the status item spins. Unlike
// a start, it waits for the subprocess (the CLI returns once the tunnel is back
// down, back up, or has failed), which is what lets the animation stop at the
// real end of the action instead of after a guessed delay.
func (c *statusBarController) runAction(tr *opTracker, names, args []string, watchdog time.Duration) {
	token := tr.begin(names)
	c.startSpinner()
	applekit.RunOnMain(c.updateConnectionActions)
	c.refresh() // the spinner owns the glyph from here
	safeGo(func() {
		done := make(chan struct{})
		var out string
		safeGo(func() {
			defer close(done)
			// Streamed rather than captured: the CLI announces each tunnel the
			// moment it is up, and that is the only signal that arrives while
			// the action is still running. Waiting for the process to exit
			// meant waiting for the slowest profile — minutes, if one of them
			// needs a login — with the header naming tunnels that were up long
			// ago. The status refresh behind it settles anything with no line
			// of its own: a VPN that was already running prints nothing,
			// because there was nothing to start.
			out, _ = c.run.runStreaming(c.exePath, args, func(line string) {
				name, ok := connectedNameFromLine(line)
				if !ok {
					return
				}
				tr.settled(map[string]struct{}{name: {}})
				if !tr.busy() {
					// Nothing left to animate; paint the real state now rather
					// than leaving the last spinner frame up until the next tick.
					c.refresh()
				}
			})
		})
		select {
		case <-done:
		case <-time.After(watchdog):
			// The subprocess is wedged; stop pretending we know it is still
			// working and let the real status through.
		}
		tr.end(token)
		c.refresh()
		c.reportIfDaemonMissing(out)
		c.startPollingAfterAction()
	})
}

// daemonMissingMarker is the phrase the client prints when the privileged
// daemon is not there to take the request (cli.escalateIfNeeded).
const daemonMissingMarker = "daemon, which is not running"

// reportIfDaemonMissing turns the one failure the user cannot diagnose into
// something they can act on. Without the daemon nothing privileged happens —
// no tunnel starts, no setting saves — and the menu would otherwise just go
// back to showing everything stopped, as if the click had never happened.
func (c *statusBarController) reportIfDaemonMissing(out string) {
	if !strings.Contains(out, daemonMissingMarker) {
		return
	}
	applekit.RunOnMain(func() {
		applekit.PresentAlert(c.displayName+" не может связаться со своей фоновой службой",
			"Пока служба не вернётся, ничего, что требует прав администратора, работать не будет.\n\n"+
				"Закройте "+c.displayName+" и откройте снова: приложение предложит восстановить службу "+
				"и один раз попросит пароль администратора.",
			applekit.AlertWarning)
	})
}

// actionState is what the spinner narrates: the verb, the status color that
// goes with it, and what it applies to. The colors are the same three the
// steady state uses — yellow while working towards connected, red while the
// tunnels go away — so the bar never invents a fourth meaning for a color.
type actionState struct {
	verb  string
	color string
	names []string
}

// label is the verb as the menu shows it. verb itself stays English: the main
// window reads it back from mainState.Action to tell the phases apart.
func (a actionState) label() string {
	switch a.verb {
	case "Quitting":
		return "Выход"
	case "Restarting":
		return "Переподключение"
	case "Connecting":
		return "Подключение"
	case "Disconnecting":
		return "Отключение"
	}
	return a.verb
}

// currentAction picks the action to show. Quitting outranks everything (the app
// is on its way out), then a restart, because it performs the other two on its
// way. Between a connect and a disconnect the connect wins: it is the one the
// user waits on, wondering whether the click landed.
func (c *statusBarController) currentAction() (actionState, bool) {
	switch {
	case c.quitting.busy():
		return actionState{"Quitting", "stop", nil}, true
	case c.restart.busy():
		return actionState{"Restarting", "yellow", c.restart.names()}, true
	case c.starting.busy():
		return actionState{"Connecting", "yellow", c.starting.names()}, true
	case c.stopping.busy():
		return actionState{"Disconnecting", "stop", c.stopping.names()}, true
	}
	return actionState{}, false
}

// startSpinner animates the status glyph (and the menu header, so an open menu
// animates too) while any action is in flight. At most one goroutine runs; it
// exits once the last action ends, leaving the following refresh to paint the
// real state.
func (c *statusBarController) startSpinner() {
	c.mu.Lock()
	if c.spinning {
		c.mu.Unlock()
		return
	}
	c.spinning = true
	c.mu.Unlock()

	safeGo(func() {
		defer func() {
			c.mu.Lock()
			c.spinning = false
			c.mu.Unlock()
		}()
		t := time.NewTicker(spinnerInterval)
		defer t.Stop()
		for tick := 0; ; tick++ {
			act, ok := c.currentAction()
			if !ok {
				return
			}
			alpha := spinnerAlpha(tick)
			applekit.RunOnMain(func() {
				color := applekit.StatusColor(act.color)
				c.setBarIcon(act.color, alpha)
				c.headerItem.SetColoredTitle(act.label()+"…", color)
			})
			<-t.C
		}
	})
}

// ---------------------------------------------------------------------------
// Connection actions and configuration panels.
// ---------------------------------------------------------------------------

// startSelected connects the configured sing-box instance after its UUID check.
func (c *statusBarController) startSelected(selected []string) {
	if len(selected) == 0 {
		return
	}
	if !c.preflightStart(selected, func() { c.startSelected(selected) }) {
		return
	}
	c.runStart(selected, append([]string{"start"}, selected...))
}

// preflightStart opens the config panel when a launch still needs a UUID and
// reports whether the launch may go ahead. On false the panel is up and retry
// runs once the user saves it, which re-enters this check.
func (c *statusBarController) preflightStart(names []string, retry func()) bool {
	cfg, _ := c.loadConfig()
	if len(names) > 0 && singboxUUID(cfg) == "" {
		c.openSingboxThenStart(retry)
		return false
	}
	return true
}

// openSingboxThenStart shows the sing-box config panel; on Apply it writes the
// config and then retries the launch (which now sees a UUID and proceeds).
func (c *statusBarController) openSingboxThenStart(retry func()) {
	if c.singboxP != nil {
		c.singboxP.Show()
		return
	}
	cfg, _ := c.loadConfig()
	panel, err := showUUIDEditPanel(singboxUUID(cfg),
		func(uuid string) error {
			res := c.run.runWithStdin(c.exePath, []string{setSingboxConfigCommand}, uuid)
			if res.status != 0 {
				return fmt.Errorf("Could not save UUID (exit %d): %s", res.status, strings.TrimSpace(res.stderr))
			}
			applekit.RunOnMain(retry)
			return nil
		},
		func() { c.singboxP = nil },
	)
	if err != nil {
		c.alertFail("UUID пользователя", err.Error(), 0)
		return
	}
	c.singboxP = panel
}

func (c *statusBarController) cmdRestart() {
	if _, busy := c.currentAction(); busy {
		return
	}
	cfg, paths := c.loadConfig()
	started := startedVPNs(cfg, paths)
	if len(started) == 0 {
		c.restartItem.SetEnabled(false)
		c.clearTransients()
		c.refresh()
		return
	}
	c.runRestart(started, []string{"-restart"})
}

// alertFail shows a themed warning alert on the main thread for a failed command.
func (c *statusBarController) alertFail(title, out string, rc int) {
	body := alertBody(out, rc)
	applekit.RunOnMain(func() { applekit.PresentAlert(title, body, applekit.AlertWarning) })
}

// appAutostart runs the (un)install as the login user when the bar is root with
// a SUDO_USER, else directly. Ports cmdInstall/UninstallAppAutostart. The
// install path bootstraps a fresh LaunchAgent instance which, via the
// single-instance guard, makes this process exit; refresh is best-effort and
// must never crash the bar, hence safeGo.
func (c *statusBarController) appAutostart(subcmd string) {
	safeGo(func() {
		var status int
		su := c.run.getenv("SUDO_USER")
		if c.run.isRoot() && su != "" {
			status = c.run.runAsUserAndWait(su, c.exePath, []string{subcmd})
		} else {
			status = c.run.runNoSudoAndWait(c.exePath, []string{subcmd})
		}
		if status == 0 {
			c.refresh()
		}
	})
}

func (c *statusBarController) quit() {
	// Quit is not instant — it stops every tunnel, drops the sing-box cache and
	// unloads the LaunchAgent first, which is seconds of work the user used to
	// spend staring at an unchanged menu bar wondering whether the click
	// registered. The spinner runs until the process is gone, so there is no
	// token to end here.
	c.quitting.begin(nil)
	c.startSpinner()
	c.updateConnectionActions()

	// The other actions bound their spinner with a watchdog that falls back to
	// real status; a Quit has nowhere to fall back to, so its watchdog leaves by
	// the other door. Without it a wedged `sudo` (no NOPASSWD rule, a process
	// that will not die) leaves the bar spinning "Quitting…" with no way out but
	// Force Quit — a worse failure than the dead click this animation replaced.
	safeGo(func() {
		time.Sleep(quitWatchdog)
		fmt.Fprintln(os.Stderr, "quit: still shutting down after "+quitWatchdog.String()+" — exiting anyway")
		os.Exit(0)
	})

	safeGo(func() {
		// Best-effort stop of all VPNs before exiting.
		c.run.runAndCaptureOut(c.exePath, []string{"stop", "all"})
		// Full shutdown is the one moment the persistent sing-box DNS cache is
		// dropped — ordinary stops keep it so reconnects start with a warm cache.
		c.run.runAndCaptureOut(c.exePath, []string{"clear-singbox-cache"})

		// Always unload the menu-bar LaunchAgent so Quit really quits — its
		// KeepAlive must not respawn the bar this session. The plist is left in
		// place, so the "enabled" state is remembered and re-applied (re-loaded)
		// on the next app launch. Ports the bootout-app-launchagent step of quit.
		su := c.run.getenv("SUDO_USER")
		if c.run.isRoot() && su != "" {
			c.run.runAsUserAndWait(su, c.exePath, []string{"bootout-app-launchagent"})
		} else {
			c.run.runNoSudoAndWait(c.exePath, []string{"bootout-app-launchagent"})
		}
		applekit.RunOnMain(func() { c.app.Terminate() })
	})
}

// cmdSetSingboxConfig opens the sing-box config panel and applies it via the
// set-singbox-config subcommand. Ports cmdSetSingboxConfig.
func (c *statusBarController) cmdSetSingboxConfig() {
	c.openSingboxThenStart(func() {
		cfg, paths := c.loadConfig()
		running := startedVPNs(cfg, paths)
		if len(running) > 0 {
			c.runRestart(running, append([]string{"restart"}, running...))
		} else {
			c.refresh()
		}
	})
}

// cmdRouteRules opens the route-rules panel and applies it via the
// set-route-rules subcommand. Ports cmdRouteRules.
func (c *statusBarController) cmdRouteRules() {
	cfg, _ := c.loadConfig()
	init := routeRulesFromConfig(cfg)
	if c.routeP != nil {
		c.routeP.Show()
		return
	}
	panel, err := showRouteRulesPanel(init,
		func(r routeRulesInput) error {
			res := c.run.runWithStdin(c.exePath, []string{"set-route-rules"}, routeRulesStdin(r))
			if res.status != 0 {
				return fmt.Errorf("%s", strings.TrimSpace(res.stderr))
			}
			applekit.RunOnMain(c.refresh)
			return nil
		},
		func() { c.routeP = nil },
	)
	if err != nil {
		applekit.PresentAlert("Не удалось сохранить исключения", err.Error(), applekit.AlertWarning)
		return
	}
	c.routeP = panel
}

// ---------------------------------------------------------------------------
// lastStart bookkeeping (guards the "Initialising…" window).
// ---------------------------------------------------------------------------

func (c *statusBarController) markStarted() {
	c.mu.Lock()
	c.lastStart = time.Now()
	c.lastStop = time.Time{}
	c.mu.Unlock()
}

func (c *statusBarController) markStopped() {
	c.mu.Lock()
	c.lastStop = time.Now()
	c.lastStart = time.Time{}
	c.mu.Unlock()
}

// clearTransients drops the start/stop timestamps once the system is idle.
func (c *statusBarController) clearTransients() {
	c.mu.Lock()
	c.lastStart = time.Time{}
	c.lastStop = time.Time{}
	c.mu.Unlock()
}

// recentlyStarted reports whether a start happened within the last 20s. Paired
// with recentlyStopped it decides when the bar is idle enough to forget both
// timestamps (clearTransients).
func (c *statusBarController) recentlyStarted() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.lastStart.IsZero() && time.Since(c.lastStart) < 20*time.Second
}

// recentlyStopped reports whether a stop happened within the last 10s. Paired
// with recentlyStarted it decides when the bar is idle enough to forget both
// timestamps (clearTransients).
func (c *statusBarController) recentlyStopped() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.lastStop.IsZero() && time.Since(c.lastStop) < 10*time.Second
}

// startPollingAfterAction refreshes the status every 2s for ~30s after a
// start/stop/restart so the bar reflects the VPN connecting/disconnecting
// without the user reopening the menu. Ports startPollingAfterAction.
func (c *statusBarController) startPollingAfterAction() {
	safeGo(func() {
		for i := 0; i < 15; i++ {
			time.Sleep(2 * time.Second)
			c.refresh()
		}
	})
}

// cmdOpenMainWindow opens the main window: the connect ring, status and
// actions, drawn from this client's own status output.
func (c *statusBarController) cmdOpenMainWindow() {
	if c.mainP != nil {
		c.mainP.Show()
		return
	}
	panel, err := showMainPanel(c.mainWindowState, c.mainWindowAction, c.ipInfo, func() { c.mainP = nil })
	if err != nil {
		applekit.PresentAlert("Не удалось открыть окно", err.Error(), applekit.AlertWarning)
		return
	}
	c.mainP = panel
}

// ipInfoTTL is how long a lookup stands: the services rate-limit, and the exit
// address only changes when the tunnel does — which is when the card forces one.
const ipInfoTTL = 5 * time.Minute

// ipInfo answers the window's IP-information card. The cache is the bar's, so
// closing and reopening the window does not cost another request.
func (c *statusBarController) ipInfo(force bool) (ipinfo.Info, error) {
	c.mu.Lock()
	if c.ipCache == nil {
		c.ipCache = ipinfo.NewCache(ipInfoTTL, nil)
	}
	cache := c.ipCache
	c.mu.Unlock()
	return cache.Get(context.Background(), force)
}

// invalidateIPInfo drops the cached exit address, the window's and the menu's.
func (c *statusBarController) invalidateIPInfo() {
	c.mu.Lock()
	cache := c.ipCache
	c.cachedIP = ""
	c.ipGen++
	c.mu.Unlock()
	if cache != nil {
		cache.Invalidate()
	}
}

// singboxProfileRetry is how often a profile that is already cached is
// downloaded again. The refresh loop runs every 15s, which is far more often
// than a server-side profile changes and often enough to hammer a broken
// network.
const singboxProfileRetry = 5 * time.Minute

// ensureRemoteProfile keeps the cache the panels read in step with the server.
//
// With nothing cached it retries on every pass: the bar and the daemon start
// independently, a request made before the daemon is listening is refused, and
// until one succeeds the panels have nothing to draw. With a profile in hand the
// point is only to follow the server — a panel opened to see what changed there
// used to show the state from before the change, because the cache moved only
// when someone connected — and once every singboxProfileRetry is often enough.
func (c *statusBarController) ensureRemoteProfile() {
	cfg, paths := c.loadConfig()
	if len(availableVPNs(cfg, paths)) == 0 {
		return
	}
	cached := len(singboxAvailableOutbounds(cfg)) > 0
	c.mu.Lock()
	if c.profileFetching ||
		(cached && !c.lastProfileFetch.IsZero() && time.Since(c.lastProfileFetch) < singboxProfileRetry) {
		c.mu.Unlock()
		return
	}
	c.profileFetching = true
	c.lastProfileFetch = time.Now()
	c.mu.Unlock()

	safeGo(func() {
		defer func() {
			c.mu.Lock()
			c.profileFetching = false
			c.mu.Unlock()
		}()
		c.refreshRemoteProfile()
	})
}

func (c *statusBarController) refreshRemoteProfile() {
	// Through the daemon: the config directory is root's, so a write from here
	// is dropped on the floor — which is how a fresh install ended up with no
	// profile and panels that said so.
	if out, status := c.run.runAndCaptureOutErr(c.exePath, []string{"refresh-profile"}); status != 0 {
		c.logWarn("sing-box profile: " + strings.TrimSpace(out))
		return
	}
	applekit.RunOnMain(c.refresh)
}

// logWarn writes a line where the bar's own warnings go.
func (c *statusBarController) logWarn(message string) {
	fmt.Fprintln(os.Stderr, "WARN: "+message)
}

// singboxVPNNames is what the window's ring acts on: the client's own sing-box
// VPN rather than every profile the config happens to list.
func (c *statusBarController) singboxVPNNames() []string {
	cfg, paths := c.loadConfig()
	var names []string
	for _, vpn := range availableVPNs(cfg, paths) {
		if vpn.Type == string(core.VPNSingbox) {
			names = append(names, vpn.Name)
		}
	}
	return names
}

// mainWindowState answers the window's poll. It runs the same `status` the menu
// bar reads, so the window and the menu never disagree.
func (c *statusBarController) mainWindowState() mainState {
	cfg, paths := c.loadConfig()
	out, rc := c.run.runExeStatusCapture(c.exePath, true)
	var running []string
	ms := parseMenuStatus("")
	if rc == 0 {
		running = parseRunningVPNs(out)
		ms = parseMenuStatus(out)
	}
	state := mainStateOf(cfg, paths, ms, running, c.exitIPForDisplay(hasSingboxRunning(cfg, running)))
	if action, busy := c.currentAction(); busy {
		state.Action = action.verb
	}
	return state
}

// mainWindowAction performs one press from the window.
func (c *statusBarController) mainWindowAction(action mainAction) error {
	switch action.Action {
	case "connect":
		names := c.singboxVPNNames()
		if len(names) == 0 {
			return fmt.Errorf("no sing-box VPN is configured")
		}
		applekit.RunOnMain(func() { c.startSelected(names) })
	case "disconnect":
		running := c.mainWindowState().Running
		if len(running) == 0 {
			return nil
		}
		applekit.RunOnMain(func() { c.runStop(running, append([]string{"stop"}, running...)) })
	case "theme":
		// Remembered for the panels: each window is its own browser origin, so
		// they cannot share the choice between themselves.
		writeThemeMode(stringValue(action.Value))
	case "vpn-autostart":
		subcmd := "uninstall-autostart"
		if boolValue(action.Value) {
			subcmd = "install-autostart"
		}
		c.vpnAutostart(subcmd)
	case "app-autostart":
		subcmd := "uninstall-app-autostart"
		if boolValue(action.Value) {
			subcmd = "install-app-autostart"
		}
		c.appAutostart(subcmd)
	case "open":
		// Every one of these opens a window of its own, which has to happen on
		// the main thread — this call is on an HTTP handler's goroutine.
		switch stringValue(action.Value) {
		case "set-uuid":
			applekit.RunOnMain(c.cmdSetSingboxConfig)
		case "route-rules":
			applekit.RunOnMain(c.cmdRouteRules)
		default:
			return fmt.Errorf("unknown panel")
		}
	default:
		return fmt.Errorf("unknown action")
	}
	return nil
}

// hasSingboxRunning reports whether one of the running VPNs is the sing-box one,
// which is what makes an exit IP meaningful.
func hasSingboxRunning(cfg *core.AppConfig, running []string) bool {
	if cfg == nil {
		return false
	}
	for _, name := range running {
		for _, vpn := range cfg.VPNs {
			if vpn.Name == name && vpn.Type == core.VPNSingbox {
				return true
			}
		}
	}
	return false
}
