#!/bin/bash
# Finder launcher for /Applications/Jorbique VPN Extended.app — runs the installed binary under /usr/local/libexec/jorbique-vpn/.
#
# Normal launch: start the menu bar as the logged-in user, straight away. The app
# needs no privilege of its own — the operations that do go to the background
# service, which the package installs.
#
# The one exception is a machine where that service is missing: someone
# installed by copying the .app, or removed the LaunchDaemon by hand. Then this
# asks for an administrator password once, runs the binary as root to put the
# service back, and that root process spawns the menu bar as the console user.
#
# macOS Finder passes -psn_* argv; never forward it into the client (unknown CLI command).
#
# If the user passes real CLI args (e.g. open -a "Jorbique VPN Extended.app" --args status), skip the admin wrapper and exec directly.
set -e
BIN=/usr/local/libexec/jorbique-vpn/JorbiqueVPN
DAEMON_PLIST=/Library/LaunchDaemons/one.jorbique.vpn.plist

# Tests point these at a temporary tree. Only the unprivileged path reads them:
# what the administrator prompt runs as root is the installed binary above,
# never something an environment variable chose.
UNPRIV_BIN="${JORBIQUE_VPN_LAUNCHER_BIN:-$BIN}"
DAEMON_PLIST="${JORBIQUE_VPN_LAUNCHER_PLIST:-$DAEMON_PLIST}"

forward_args=()
for a in "$@"; do
	[[ "$a" == -psn_* ]] && continue
	forward_args+=("$a")
done

exec_real_bin() {
	if [[ ${#forward_args[@]} -eq 0 ]]; then
		exec "$UNPRIV_BIN"
	fi
	exec "$UNPRIV_BIN" "${forward_args[@]}"
}

if [ ! -x "$UNPRIV_BIN" ]; then
	/usr/bin/osascript -e 'display dialog "Jorbique VPN Extended: the client binary was not found. Reinstall the .pkg." buttons {"OK"} default button "OK" with title "Jorbique VPN Extended"' 2>/dev/null || true
	exit 1
fi

if [ "$(/usr/bin/id -u)" -eq 0 ]; then
	exec_real_bin
fi

if [[ ${#forward_args[@]} -gt 0 ]]; then
	exec_real_bin
fi

CONSOLE_USER="$(/usr/bin/stat -f '%Su' /dev/console 2>/dev/null || true)"
case "$CONSOLE_USER" in
	"" | root | loginwindow | _mbsetupuser) CONSOLE_USER="$(/usr/bin/id -un)" ;;
esac

# Set up already means two things, and the second is the one that bites: the
# plist can be on disk while launchd has no job — an upgrade that could not
# reload it, or someone who booted it out. The menu bar would come up unable to
# start a tunnel or save a setting, and the click would look ignored. So the
# check is "is the service actually there", and if it is not, this goes through
# the administrator prompt and puts it back.
DAEMON_LABEL="${JORBIQUE_VPN_LAUNCHER_LABEL:-one.jorbique.vpn}"
if [ -f "$DAEMON_PLIST" ] && /bin/launchctl print "system/${DAEMON_LABEL}" >/dev/null 2>&1; then
	# Nothing here needs root: no password, and no waiting on a privileged step
	# that has nothing to do.
	#
	# Started detached, not exec'd in place. A Finder-launched .app that execs a
	# binary outside its bundle keeps the process but loses the bundle: the Dock
	# icon disappears and no status item ever appears, while the process sits
	# there running its polling loop as if all were well. Spawning a child and
	# letting this exit is how the menu bar has always been started.
	#
	# Started through the .app, not through libexec: JorbiqueVPN next to this
	# script is a symlink to $BIN, and a process whose executable path is inside
	# Contents/MacOS gets the bundle as its main bundle. That identity is what
	# names the app and draws its icon on the window in Stage Manager and
	# Mission Control; a bare binary has neither. Falls back to the installed
	# path when the symlink is missing (a hand-copied .app, an old install).
	#
	# Exec'd in place when the symlink is there: the path stays inside the
	# bundle, so the bundle identity holds, and the process stays the one macOS
	# launched. That matters for a jorbique:// link from the /sub page: macOS
	# hands the link (a GetURL Apple Event) to the process it started, and a
	# detached child never saw it — the first click only opened the app.
	if [ -z "${JORBIQUE_VPN_LAUNCHER_BIN:-}" ]; then
		BUNDLE_BIN="$(/usr/bin/dirname "$0")/JorbiqueVPN"
		if [ -x "$BUNDLE_BIN" ]; then
			exec "$BUNDLE_BIN" --menu-bar </dev/null >/dev/null 2>&1
		fi
	fi
	"$UNPRIV_BIN" --menu-bar >/dev/null 2>&1 &
	exit 0
fi

JORBIQUE_VPN_FIRST_RUN=1
# Tests take the decision and stop here: what follows opens a dialog and waits
# for a person, which in a test run means a minute of nothing.
if [ -n "${JORBIQUE_VPN_LAUNCHER_DRY_RUN:-}" ]; then
	echo "setup required: no background service loaded (${DAEMON_PLIST})"
	exit 0
fi

export JORBIQUE_VPN_BIN="$BIN"
export JORBIQUE_VPN_USER="$CONSOLE_USER"
export JORBIQUE_VPN_FIRST_RUN

/usr/bin/osascript <<'APPLESCRIPT' || exit 1
on run
	set binPath to (system attribute "JORBIQUE_VPN_BIN")
	set u to (system attribute "JORBIQUE_VPN_USER")
	set fr to (system attribute "JORBIQUE_VPN_FIRST_RUN")
	if binPath is missing value or binPath is "" then
		display dialog "Jorbique VPN Extended: internal error (binary path is missing)." buttons {"OK"} default button "OK" with title "Jorbique VPN Extended"
		return
	end if
	if u is missing value or u is "" then
		display dialog "Jorbique VPN Extended: could not detect the console user." buttons {"OK"} default button "OK" with title "Jorbique VPN Extended"
		return
	end if
	if fr is "1" then
		set intro to "Jorbique VPN Extended needs to set up its background service." & return & return & "macOS will ask for an administrator password or Touch ID, just like when installing software." & return & return & "This service performs the operations that need administrator rights. The app itself runs as you, and will not ask again while the service is in place." & return & return & "Continue?"
		try
			set d to display dialog intro buttons {"Cancel", "Continue"} default button "Continue" with title "Jorbique VPN Extended setup" with icon caution
			if button returned of d is "Cancel" then
				return
			end if
		on error number errNum
			if errNum is -128 then
				return
			end if
		end try
	end if
	try
		set authPrompt to "Jorbique VPN Extended: checking the background service, then starting the VPN manager."
		if fr is "1" then
			set authPrompt to "Jorbique VPN Extended: installing the background service and starting the VPN manager."
		end if
		do shell script "export SUDO_USER=" & quoted form of u & " && exec " & quoted form of binPath with administrator privileges with prompt authPrompt
	on error errMsg number errNum
		if errNum is -128 then
			return
		end if
		display dialog errMsg buttons {"OK"} default button "OK" with title "Jorbique VPN Extended"
	end try
end run
APPLESCRIPT

exit 0
