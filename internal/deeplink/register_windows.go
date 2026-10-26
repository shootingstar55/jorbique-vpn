//go:build windows

package deeplink

import (
	"golang.org/x/sys/windows/registry"
)

// Register makes Windows open jorbique:// links with exe, for the current user
// (HKCU\Software\Classes needs no administrator rights of its own). The client
// is a portable zip with no installer, so this runs on every start: the key
// then always points at the exe the user last ran, wherever they unpacked it.
func Register(exe string) error {
	root, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Classes\`+Scheme, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.SetStringValue("", "URL:Jorbique VPN"); err != nil {
		return err
	}
	if err := root.SetStringValue("URL Protocol", ""); err != nil {
		return err
	}
	icon, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Classes\`+Scheme+`\DefaultIcon`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer icon.Close()
	if err := icon.SetStringValue("", `"`+exe+`",0`); err != nil {
		return err
	}
	command, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Classes\`+Scheme+`\shell\open\command`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer command.Close()
	return command.SetStringValue("", `"`+exe+`" "%1"`)
}
