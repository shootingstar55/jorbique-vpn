//go:build darwin

package ui

import (
	"fmt"
	"io/fs"

	"jorbiquevpn/frontend"
	"jorbiquevpn/internal/localapi"
	"jorbiquevpn/internal/macos/applekit"
)

// webPanel is a window showing one of the shared Vue pages. Every panel in this
// client is one of these now: the forms are the same Naive UI the Windows client
// draws, served from the embedded bundle over a private loopback origin, so a
// change to a form lands on both platforms at once.
type webPanel struct {
	window applekit.Window
	server *localapi.LocalServer
}

// showWebPanel opens a window of the given size and fills it with the page the
// server serves.
//
// start receives the function that closes this window, because the page's Cancel
// button reaches the window through the server: the two are built in one step to
// keep either from existing without the other.
func showWebPanel(title string, width, height, minWidth, minHeight applekit.CGFloat, start func(closeWindow func()) (*localapi.LocalServer, error), onClosed func()) (*webPanel, error) {
	window := applekit.NewPanel(width, height, title)
	window.SetResizable()
	window.SetContentMinSize(minWidth, minHeight)
	panel := &webPanel{window: window}

	server, err := start(func() { applekit.RunOnMain(func() { panel.window.Close() }) })
	if err != nil {
		window.Release()
		return nil, err
	}
	webView, err := applekit.NewWebView(server.URL, applekit.NewRect(0, 0, width, height))
	if err != nil {
		server.Close()
		window.Release()
		return nil, err
	}
	panel.server = server
	window.AddSubview(webView)
	webView.Release()
	window.OnClose(func() {
		server.Close()
		defer window.Release()
		if onClosed != nil {
			onClosed()
		}
	})
	window.Center()
	window.Show()
	return panel, nil
}

// SetNormalLevel takes this window out of the floating level, for the one window
// that is the app rather than a palette over it.
func (p *webPanel) SetNormalLevel() {
	if p != nil {
		p.window.SetNormalLevel()
	}
}

func (p *webPanel) Show() {
	if p != nil {
		p.window.Show()
	}
}

// frontendAssets is the embedded Vue bundle every panel is served from.
func frontendAssets() (fs.FS, error) {
	assets, err := fs.Sub(frontend.Dist, "dist")
	if err != nil {
		return nil, fmt.Errorf("UI assets: %w", err)
	}
	return assets, nil
}
