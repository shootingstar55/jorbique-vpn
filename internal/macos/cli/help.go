package cli

import (
	"fmt"
	"io"
	"path/filepath"
)

func PrintHelp(w io.Writer, prog string) {
	if prog == "" {
		prog = "jorbique-vpn"
	}
	prog = filepath.Base(prog)
	fmt.Fprintf(w, `JORBIQUE-VPN(1)                 VPN Manager                 JORBIQUE-VPN(1)

NAME
     %[1]s — macOS client for sing-box

SYNOPSIS
     %[1]s status
     %[1]s start [name|all]
     %[1]s stop [name|all]
     %[1]s restart [name|all]
     %[1]s set-singbox-config  (stdin: line1=uuid, line2=enabled outbound tags, line3=selector node, line4=mode)
     %[1]s set-route-rules     (stdin: line1=direct domains, line2=process names, line3=executable paths tab-separated for direct bypass)
     %[1]s refresh-profile
     %[1]s update-singbox
     %[1]s singbox-versions
     %[1]s -restart
     %[1]s -autostart-check
     %[1]s install-autostart
     %[1]s uninstall-autostart
     %[1]s install-app-autostart
     %[1]s uninstall-app-autostart
     %[1]s exit-ip [wait]
     %[1]s [--menu-bar]
     sudo %[1]s   (no subcommand — install the privileged daemon, then start the menu bar as your user)
     %[1]s help | -h | --help

DESCRIPTION
     Controls the sing-box VPN service on macOS. The tunnel uses the proxy
     outbounds of the profile the server publishes; every one of them moves the
     traffic's exit address. Supports wake-from-sleep and network-change
     autostart.
     Without arguments (or with --menu-bar): shows status bar icon with menu.
     First setup from a Terminal: sudo /path/to/%[1]s runs as root briefly to install
     the privileged daemon, then starts %[1]s --menu-bar as SUDO_USER (under the console
     GUI session via launchctl asuser when possible, so the tray works after .pkg / Finder launch).
     The menu bar never runs as root: operations that need it are performed by the
     daemon, which takes a fixed set of requests over a socket in /var/run/jorbique-vpn.

COMMANDS
     (root bootstrap — no subcommand)
             Run as normal macOS sudo: sudo /absolute/path/to/%[1]s
             Installs and loads the privileged daemon, then starts the
             menu-bar process as user SUDO_USER.

     status [--no-connectivity]
             Show whether the tunnel is up, the daemon and autostart state.

     start [name|all]
             Start sing-box. Without an argument, selects the configured service.

     stop [name|all]
             Stop sing-box. Without an argument, selects the configured service.

     restart [name|all]
             Restart sing-box. Without an argument, selects the configured service.

     set-singbox-config
             Downloads the remote profile and writes singbox.json.
             Reads from stdin: line1=uuid, line2=enabled outbounds (comma-separated
             tags), line3=the selector node. Tags come from the downloaded
             sing-box profile; an empty second line switches every outbound off,
             and no second line keeps the stored choice. A blank third line keeps
             what is stored. line4=the mode: "naive" connects through the
             profile's naive outbound alone, "standard" through the outbounds
             above; blank keeps what is stored.

     set-route-rules
             Set direct domains, optional process names, and optional executable paths for direct bypass.
             Stdin: line1 comma-separated domains; line2 comma-separated process names; line3 tab-separated absolute paths to executables (.app resolves to its MacOS binary).
             Writes direct_domains, direct_bypass_processes, and direct_bypass_paths to vpn_cfg.json and DNS/route rules to sing-box config(s).

     refresh-profile
             Download the sing-box profile into the config directory, so the panels can list what the server offers without connecting first. Runs as root because that directory is root's.

     update-singbox
             Stop sing-box, download the latest sing-box from the update server, and install it next to %[1]s. Needs write access to the app directory (use sudo if the bundle is under /Applications).

     singbox-versions
             Print JSON {"local","remote"}: the installed sing-box version and the version advertised by the update server (used by the menu-bar UI).

     -restart
             Stop all, then start the last list.

     -autostart-check
             Check LaunchDaemon status (installed/enabled/loaded).

     install-autostart
             Bring the tunnel up at boot and after wake (run as root).

     uninstall-autostart
             Stop bringing the tunnel up at boot (run as root).

     install-app-autostart
             Install LaunchAgent for the menu-bar app at user login (run as your user, not root).

     uninstall-app-autostart
             Remove LaunchAgent plist (login autostart off; keeps the current menu bar running).

     exit-ip [wait]
             Print JSON {"ip","cc"} using api.ip_urls (first entry is the primary), then api.geo_url, then built-in fallbacks. With wait, polls
             until the public IP is stable (or a max time) for sing-box, instead of a single request.

     help, -h, --help
             Show this help.

EXIT STATUS
     0  Success
     1  General error
     64  Usage error (invalid arguments)
     65  Data error (e.g. required tool not found)
     66  Missing file
     77  Permission denied
     78  Invalid configuration (vpn_cfg.json, sing-box, VPN definitions)

FILES
     /Library/Application Support/JorbiqueVPN/cfg/vpn_cfg.json
             Settings: the sing-box UUID, outbound choices and route rules.
     /Library/Logs/JorbiqueVPN/
             Daemon and tunnel logs.

SEE ALSO
     Sing-box is downloaded and updated by the application.

`, prog)
}
