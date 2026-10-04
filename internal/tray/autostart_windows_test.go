//go:build windows

package tray

import (
	"strings"
	"testing"
)

func TestLogonTaskXMLCarriesExeAndWorkingDir(t *testing.T) {
	exe := `C:\Users\admin\Desktop\jorbique vpn\Jorbique-VPN.exe`
	xml := logonTaskXML(exe)
	if got := taskXMLField(xml, "Command"); got != exe {
		t.Errorf("Command = %q, want %q", got, exe)
	}
	if got := taskXMLField(xml, "Arguments"); got != logonAutostartArg {
		t.Errorf("Arguments = %q, want %q", got, logonAutostartArg)
	}
	if got := taskXMLField(xml, "WorkingDirectory"); got != `C:\Users\admin\Desktop\jorbique vpn` {
		t.Errorf("WorkingDirectory = %q", got)
	}
	// The task the app checks itself against must pass its own health check.
	if !strings.Contains(strings.ToLower(xml), "highestavailable") {
		t.Error("task XML must ask for the elevated run level")
	}
}

func TestLogonTaskXMLEscapesMarkup(t *testing.T) {
	xml := logonTaskXML(`C:\a&b\Jorbique-VPN.exe`)
	if !strings.Contains(xml, `C:\a&amp;b\Jorbique-VPN.exe`) {
		t.Errorf("ampersand in the exe path must be escaped, or RegisterTask rejects the XML:\n%s", xml)
	}
}

func TestPathsEqual(t *testing.T) {
	if !pathsEqual(`C:\A\Jorbique-VPN.exe`, `c:\a\jorbique-vpn.exe`) {
		t.Fatal("expected case-insensitive path match")
	}
}
