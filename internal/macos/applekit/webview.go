//go:build darwin

package applekit

import "fmt"

// NewWebView creates a WKWebView filling frame and starts loading rawURL.
func NewWebView(rawURL string, frame Rect) (View, error) {
	if err := loadWebKit(); err != nil {
		return View{}, err
	}
	if Class("WKWebView") == 0 {
		return View{}, fmt.Errorf("WKWebView is unavailable")
	}
	configuration := cls("WKWebViewConfiguration", "alloc").Send(Sel("init"))
	webView := cls("WKWebView", "alloc").Send(Sel("initWithFrame:configuration:"), frame, configuration)
	configuration.Send(Sel("release"))
	if webView == 0 {
		return View{}, fmt.Errorf("WKWebView could not be created")
	}
	view := View{id: webView}
	view.EnableFrameLayout()
	view.SetAutoresizingMask(ViewWidthSizable | ViewHeightSizable)
	url := cls("NSURL", "URLWithString:", nsString(rawURL))
	request := cls("NSURLRequest", "requestWithURL:", url)
	webView.Send(Sel("loadRequest:"), request)
	return view, nil
}

// Release balances an alloc-owned view after its superview retained it.
func (v View) Release() { v.id.Send(Sel("release")) }
