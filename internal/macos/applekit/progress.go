//go:build darwin

package applekit

// ProgressWindow is a small dark dialog with a message label and a horizontal
// progress bar, shown while a long operation runs (the first-launch sing-box
// download). It must be created and driven on the main thread. The bar starts as
// an animated indeterminate strip and switches to a determinate fill the first
// time SetPercent is called, so it looks alive even before the first byte.
type ProgressWindow struct {
	win Window
	bar ID
	msg ID
}

// NewProgressWindow builds and shows a centered dark progress dialog.
func NewProgressWindow(title, message string) ProgressWindow {
	w := NewPanel(440, 130, title)

	// Mutable message label (borderless NSTextField).
	lbl := cls("NSTextField", "alloc").Send(Sel("initWithFrame:"), NewRect(20, 78, 400, 22))
	lbl.Send(Sel("setStringValue:"), nsString(message))
	lbl.Send(Sel("setBezeled:"), false)
	lbl.Send(Sel("setEditable:"), false)
	lbl.Send(Sel("setSelectable:"), false)
	lbl.Send(Sel("setDrawsBackground:"), false)
	lbl.Send(Sel("setTextColor:"), primaryTextColor().id)
	lbl.Send(Sel("setFont:"), cls("NSFont", "systemFontOfSize:", CGFloat(13)))
	w.AddSubview(View{id: lbl})

	// Horizontal progress bar (NSProgressIndicatorStyleBar == 0).
	bar := cls("NSProgressIndicator", "alloc").Send(Sel("initWithFrame:"), NewRect(20, 42, 400, 20))
	bar.Send(Sel("setStyle:"), 0)
	bar.Send(Sel("setIndeterminate:"), true)
	bar.Send(Sel("setMinValue:"), CGFloat(0))
	bar.Send(Sel("setMaxValue:"), CGFloat(100))
	bar.Send(Sel("startAnimation:"), nilID)
	w.AddSubview(View{id: bar})

	w.Center()
	w.Show()
	return ProgressWindow{win: w, bar: bar, msg: lbl}
}

// SetPercent fills the bar to pct (0..100), switching it to determinate on the
// first call. Call on the main thread.
func (p ProgressWindow) SetPercent(pct int) {
	if p.bar == nilID {
		return
	}
	if pct < 0 {
		pct = 0
	} else if pct > 100 {
		pct = 100
	}
	p.bar.Send(Sel("stopAnimation:"), nilID)
	p.bar.Send(Sel("setIndeterminate:"), false)
	p.bar.Send(Sel("setDoubleValue:"), CGFloat(pct))
}

// SetMessage replaces the label text. Call on the main thread.
func (p ProgressWindow) SetMessage(s string) {
	if p.msg != nilID {
		p.msg.Send(Sel("setStringValue:"), nsString(s))
	}
}

// Close dismisses and releases the dialog. Call on the main thread.
// The window is setReleasedWhenClosed:false, so the caller (this method) must
// release it manually to avoid a native object leak.
func (p ProgressWindow) Close() {
	p.win.Close()
	p.win.Release()
}
