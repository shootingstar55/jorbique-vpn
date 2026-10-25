package ui

import "testing"

// The singleton guard is the only thing standing between an upgrade and two
// menu-bar icons, and it went unnoticed for a whole rename because nothing
// exercised it. These are the command lines the bar is actually started with.
func TestIsBarCommandLine(t *testing.T) {
	const exe = "JorbiqueVPN"
	tests := []struct {
		name    string
		cmdline string
		want    bool
	}{
		{"launch agent", "/usr/local/libexec/jorbique-vpn/JorbiqueVPN --menu-bar", true},
		{"finder wrapper, no args", "/usr/local/libexec/jorbique-vpn/JorbiqueVPN", true},
		{"finder process-serial noise", "/usr/local/libexec/jorbique-vpn/JorbiqueVPN -psn_0_12345", true},
		{"same binary from another directory", "/opt/old/JorbiqueVPN --menu-bar", true},
		{"another client's bar", "/usr/local/sbin/MltVPN/MultiVPNmanager --menu-bar", false},
		{"same binary running a CLI subcommand", "/usr/local/libexec/jorbique-vpn/JorbiqueVPN status", false},
		{"same binary starting a tunnel", "/usr/local/libexec/jorbique-vpn/JorbiqueVPN start intra", false},
		{"started through the app bundle, whose path has a space in it", "/Applications/Jorbique VPN Extended.app/Contents/MacOS/JorbiqueVPN --menu-bar", true},
		{"through the bundle, no args", "/Applications/Jorbique VPN Extended.app/Contents/MacOS/JorbiqueVPN", true},
		{"through the bundle, running a CLI subcommand", "/Applications/Jorbique VPN Extended.app/Contents/MacOS/JorbiqueVPN status", false},
		{"an unrelated process that reused the pid", "/usr/bin/ssh -N host", false},
		{"a different app whose name merely contains ours", "/Applications/Other.app/Contents/MacOS/JorbiqueVPNHelper", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isBarCommandLine(tt.cmdline, exe); got != tt.want {
				t.Errorf("isBarCommandLine(%q, %q) = %v, want %v", tt.cmdline, exe, got, tt.want)
			}
		})
	}
}

// An empty name must never match everything.
func TestIsBarCommandLineEmptyExeName(t *testing.T) {
	if isBarCommandLine("/usr/local/libexec/jorbique-vpn/JorbiqueVPN --menu-bar", "") {
		t.Error("an unknown own-name must not match an arbitrary executable")
	}
}
