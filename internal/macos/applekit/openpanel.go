//go:build darwin

package applekit

import "github.com/ebitengine/purego/objc"

// nsModalResponseOK is returned by -runModal when the user confirms.
const nsModalResponseOK = 1

// ChooseApplicationFiles presents a modal NSOpenPanel rooted at /Applications
// that lets the user pick one or more files (.app bundles are files here), and
// returns their filesystem paths. Returns nil if cancelled. Ports the NSOpenPanel
// setup in RouteRulesPanel.addApplications:, using the synchronous -runModal
// instead of a sheet so no completion block is needed. Must run on the main
// thread.
func ChooseApplicationFiles() []string {
	if err := Load(); err != nil {
		return nil
	}
	panel := cls("NSOpenPanel", "openPanel")
	panel.Send(Sel("setTitle:"), nsString("Добавить приложения"))
	panel.Send(Sel("setPrompt:"), nsString("Добавить"))
	panel.Send(Sel("setCanChooseFiles:"), true)
	panel.Send(Sel("setCanChooseDirectories:"), false)
	panel.Send(Sel("setAllowsMultipleSelection:"), true)
	panel.Send(Sel("setTreatsFilePackagesAsDirectories:"), false)
	panel.Send(Sel("setDirectoryURL:"), fileURL("/Applications"))

	if intMsg(panel, "runModal") != nsModalResponseOK {
		return nil
	}
	urls := send(panel, "URLs")
	n := int(objc.Send[uint](urls, Sel("count")))
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		url := urls.Send(Sel("objectAtIndex:"), uint(i))
		if path := goStringFromNS(send(url, "path")); path != "" {
			out = append(out, path)
		}
	}
	return out
}

// fileURL builds an NSURL for a filesystem path.
func fileURL(path string) ID {
	return cls("NSURL", "fileURLWithPath:", nsString(path))
}
