//go:build darwin

package applekit

import (
	"time"
)

// activationPolicyAccessory (NSApplicationActivationPolicyAccessory): no Dock
// icon and no app menu.
const activationPolicyAccessory = 1

// nsEventMaskAny matches every event type in nextEventMatchingMask:.
const nsEventMaskAny = ^uint64(0)

// CollectLaunchURLs is for a launch that is about to hand over to another copy
// of the menu bar and exit: it checks in with AppKit just long enough to take
// the URL macOS opened the app with, so a jorbique:// link that started this
// copy is not lost with it. It returns as soon as a URL arrives, or after
// wait with none. Must run on the main thread, before anything else touches
// AppKit; the process is expected to exit afterwards.
func CollectLaunchURLs(wait time.Duration) []string {
	if err := Load(); err != nil {
		return nil
	}
	app := App{id: cls("NSApplication", "sharedApplication")}
	// Accessory: this copy is gone within a second and has no business in the Dock.
	app.id.Send(Sel("setActivationPolicy:"), activationPolicyAccessory)
	var urls []string
	app.OnOpenURLs(func(u []string) { urls = append(urls, u...) })
	app.id.Send(Sel("finishLaunching"))
	past := cls("NSDate", "distantPast")
	mode := nsString("kCFRunLoopDefaultMode")
	for deadline := time.Now().Add(wait); len(urls) == 0 && time.Now().Before(deadline); {
		// distantPast: one pass of the run loop without blocking. The GetURL
		// Apple Event is dispatched to the delegate inside this call.
		if event := app.id.Send(Sel("nextEventMatchingMask:untilDate:inMode:dequeue:"), nsEventMaskAny, past, mode, true); event != nilID {
			app.id.Send(Sel("sendEvent:"), event)
			continue
		}
		time.Sleep(25 * time.Millisecond)
	}
	return urls
}
