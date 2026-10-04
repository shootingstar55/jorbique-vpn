#!/usr/bin/env bash
# Install, upgrade, roll back and remove the macOS client.
#
# Until now the only supported move was to double-click a newer .pkg on top of
# the old one. That works right up until the new build is worse than the one it
# replaced, and then there is nothing to go back to: the previous binary is
# gone, no receipt of it is kept, and the person on the other end of the call is
# told to find an old .pkg somewhere. Uninstalling had the same problem in
# reverse — the installer leaves a LaunchDaemon, a LaunchAgent and a directory
# of downloaded configs, and nothing removes them.
#
#   status              what is installed, where, and whether a rollback exists
#   install <pkg>       first install
#   upgrade <pkg>       snapshot the current install, then install the new one;
#                       a build that fails its health check is rolled back
#   rollback            put the snapshot back
#   uninstall [--purge] remove the client; --purge takes the user's data too
#
# --dry-run prints the plan instead of running it, which is also how the tests
# read the steps without a root shell.
set -euo pipefail

# Every path hangs off a prefix so the whole thing can be pointed at a throwaway
# tree. That is what makes an upgrade-and-roll-back exercise testable: the test
# needs an install it is allowed to replace and then restore, and a real machine
# is not one. Empty everywhere else, which is the machine itself.
prefix="${JORBIQUE_VPN_ADMIN_PREFIX:-}"

install_dir="${prefix}/usr/local/libexec/jorbique-vpn"
bin_dir="${prefix}/usr/local/bin"
support_dir="${prefix}/Library/Application Support/JorbiqueVPN"
logs_dir="${prefix}/Library/Logs/JorbiqueVPN"
caches_dir="${prefix}/Library/Caches/JorbiqueVPN"
app_bundle="${prefix}/Applications/Jorbique VPN.app"
daemon_label="one.jorbique.vpn"
bar_label="one.jorbique.vpn.bar"
daemon_plist="${prefix}/Library/LaunchDaemons/${daemon_label}.plist"
# The menu-bar agent is per-user and the client regenerates it on launch, so a
# rollback does not depend on it — but a snapshot that skips it is one the
# person reading it has to reason about, and a plist naming a binary path from
# the other layout is exactly the thing worth restoring verbatim.
bar_plist="${HOME}/Library/LaunchAgents/${bar_label}.plist"
pkg_id="one.jorbique.vpn.pkg"
# Snapshots live outside the install directory: a reinstall replaces that
# directory wholesale, and a rollback copy inside it would go with it.
rollback_dir="${support_dir}/rollback"
# The daemon's control socket: the menu bar asks it for anything needing root.
control_socket="${prefix}/var/run/jorbique-vpn/control.sock"

dry_run=0
purge=0
# Set by `try` when a step of a restore fails. A restore runs on a machine that
# is already between two builds, so it is the one place that carries on past a
# failing step and reports at the end instead of stopping at the first one.
restore_failed=0
# Only one of these may be touching the install at a time. Two upgrades started
# together snapshot each other's half-replaced trees, and an upgrade racing a
# rollback leaves a libexec from one build beside a config from the other.
# macOS ships no flock(1), so the lock is a directory: mkdir is the atomic
# test-and-set every POSIX shell has.
lock_dir="${support_dir}/.admin.lock"
held_lock=""

# An upgrade that went wrong is read about afterwards, usually by someone who
# was not watching the terminal. Everything that is printed is also appended to
# the log next to the client's own, with the command line that started it.
admin_log="${logs_dir}/admin.log"

write_log() {
	[ "${dry_run}" -eq 1 ] && return 0
	mkdir -p "${logs_dir}" 2>/dev/null || return 0
	printf '%s %s\n' "$(date '+%Y-%m-%dT%H:%M:%S%z')" "$*" >>"${admin_log}" 2>/dev/null || true
}

log() {
	printf '%s\n' "$*" >&2
	write_log "$*"
}

die() {
	printf 'error: %s\n' "$*" >&2
	write_log "error: $*"
	exit 1
}

# run executes a step, and names it either way: under --dry-run the plan is all
# there is, and on a real run the same line is what tells the reader afterwards
# how far it got. Every state-changing command goes through it, so the plan and
# the real thing cannot drift.
run() {
	if [ "${dry_run}" -eq 1 ]; then
		printf '+ %s\n' "$*"
		return 0
	fi
	printf '+ %s\n' "$*" >&2
	write_log "+ $*"
	"$@"
}

# try is `run` for the steps of a restore. errexit is right everywhere else —
# stopping is the safe answer when a step of an upgrade fails, because the
# snapshot is still there. It is the wrong answer during the restore of that
# snapshot: the process would die halfway through, with the daemon booted out,
# the install directory half-written and nothing said about which of the two
# builds the machine is on. So every step is attempted, each failure is named,
# and the caller decides what to tell the person reading.
try() {
	if run "$@"; then
		return 0
	fi
	log "restore: this step failed: $*"
	restore_failed=1
	return 0
}

# proc_start_time is when a pid began, as the system reports it. It is what
# makes a pid mean one particular process: pids are handed out again, and after
# a power loss the machine comes back renumbering from the start, so the number
# in a lock written before the crash stands a good chance of belonging to
# something else by the time anyone reads it.
proc_start_time() {
	ps -o lstart= -p "$1" 2>/dev/null | tr -d '\n'
}

# take_lock claims the install for this process. Stale locks are reclaimed: the
# pid inside, together with the moment that pid started, is what tells a lock
# left by a machine that lost power apart from one held by an upgrade that is
# still running.
take_lock() {
	[ "${dry_run}" -eq 1 ] && return 0
	mkdir -p "${support_dir}" 2>/dev/null || true
	if ! mkdir "${lock_dir}" 2>/dev/null; then
		local owner started now
		owner="$(cat "${lock_dir}/pid" 2>/dev/null || true)"
		# Only a real pid is asked about. `kill -0 0` signals this process's
		# whole group and succeeds, and `kill -0 -1` reaches every process the
		# user owns — either would report a lock as held forever, on a machine
		# whose pid file is empty or corrupt.
		case "${owner}" in
		'' | *[!0-9]*) owner="" ;;
		0) owner="" ;;
		esac
		if [ -n "${owner}" ] && kill -0 "${owner}" 2>/dev/null; then
			started="$(cat "${lock_dir}/start" 2>/dev/null || true)"
			now="$(proc_start_time "${owner}")"
			# A lock from an older version of this script records no start time.
			# Then the pid is all there is, and blocking is the safe reading:
			# refusing an upgrade is recoverable, two of them running is not.
			if [ -z "${started}" ] || [ "${started}" = "${now}" ]; then
				die "another jorbique-vpn-admin (pid ${owner}) is working on this install; wait for it to finish"
			fi
			log "clearing a lock whose pid ${owner} now belongs to a process started at ${now}"
		else
			log "clearing a lock left behind by pid ${owner:-unknown}"
		fi
		rm -rf "${lock_dir}"
		mkdir "${lock_dir}" 2>/dev/null || die "cannot take the install lock at ${lock_dir}"
	fi
	printf '%s\n' "$$" >"${lock_dir}/pid" 2>/dev/null || true
	printf '%s\n' "$(proc_start_time "$$")" >"${lock_dir}/start" 2>/dev/null || true
	held_lock="${lock_dir}"
	trap release_lock EXIT INT TERM
}

release_lock() {
	[ -n "${held_lock}" ] || return 0
	rm -rf "${held_lock}" 2>/dev/null || true
	held_lock=""
}

need_root() {
	[ "${dry_run}" -eq 1 ] && return 0
	# A tree under /tmp belongs to whoever made it; asking for root there would
	# only mean the tests cannot run.
	[ -n "${prefix}" ] && return 0
	[ "$(id -u)" -eq 0 ] || die "run this with sudo: sudo $0 $*"
}

installed_version() {
	pkgutil --pkg-info "${pkg_id}" 2>/dev/null | awk '/^version:/ { print $2 }'
}

snapshot_version() {
	if [ -f "${rollback_dir}/VERSION" ]; then
		cat "${rollback_dir}/VERSION" 2>/dev/null
	fi
}

# pkg_version reads the version out of an unopened .pkg, so an upgrade can name
# both ends of the move. `installer -pkginfo` prints the package's title, not its
# version, so the version comes from the PackageInfo inside the archive — the
# same field pkgutil reports once it is installed.
pkg_version() {
	local pkg="$1" tmp
	[ -f "${pkg}" ] || return 0
	tmp="$(mktemp -d)"
	if pkgutil --expand "${pkg}" "${tmp}/x" >/dev/null 2>&1; then
		# pkgbuild writes one PackageInfo; a distribution package has one per
		# component, and the app's own is the one that is not the 1.0 wrapper.
		grep -ho 'version="[0-9][0-9.]*"' "${tmp}/x/PackageInfo" "${tmp}/x"/*/PackageInfo 2>/dev/null |
			sed -e 's/version="//' -e 's/"//' | grep -v '^1\.0$' | head -n 1
	fi
	rm -rf "${tmp}"
}

cmd_status() {
	local cur snap bin
	cur="$(installed_version || true)"
	snap="$(snapshot_version || true)"
	bin="$(client_binary)"
	# A rollback forgets the package receipt — the files on disk are not the
	# version it named any more — so a machine that was rolled back has a client
	# and no receipt to read a version from. Say so rather than "not installed".
	if [ -z "${cur}" ] && [ -n "${bin}" ]; then
		cur="${snap:-unknown} (restored by a rollback; no package receipt)"
	fi
	printf 'installed:   %s\n' "${cur:-<not installed>}"
	printf 'binaries:    %s\n' "$(if [ -n "${bin}" ]; then dirname "${bin}"; else printf '%s' "${install_dir}"; fi)"
	printf 'data:        %s\n' "${support_dir}"
	printf 'rollback to: %s\n' "${snap:-<none>}"
	printf 'daemon:      %s\n' "$(launchctl print "system/${daemon_label}" >/dev/null 2>&1 && echo loaded || echo "not loaded")"
	printf 'menu bar:    %s\n' "$(launchctl print "gui/$(id -u)/${bar_label}" >/dev/null 2>&1 && echo loaded || echo "not loaded")"
	printf 'control:     %s\n' "$([ -S "${control_socket}" ] && echo "${control_socket}" || echo "<no socket — privileged operations will fail>")"
}

# client_binary is the installed client, or nothing when there is none.
client_binary() {
	if [ -x "${install_dir}/JorbiqueVPN" ]; then
		printf '%s\n' "${install_dir}/JorbiqueVPN"
	fi
}

# stop_client leaves no tunnel behind before the files under it change. The plan
# names the step even where the binary is missing: --dry-run is read on machines
# that have never had the client, and a plan that changes shape with the state of
# the machine printing it is not one anybody can review.
stop_client() {
	local bin
	bin="$(client_binary)"
	if [ "${dry_run}" -eq 1 ]; then
		run "${install_dir}/JorbiqueVPN" stop all || true
	elif [ -n "${bin}" ]; then
		run "${bin}" stop all || true
	fi
	run launchctl bootout "system/${daemon_label}" || true
}

take_snapshot() {
	local version="$1"
	run rm -rf "${rollback_dir}"
	run mkdir -p "${rollback_dir}"
	if [ "${dry_run}" -eq 1 ] || [ -d "${install_dir}" ]; then
		run ditto --rsrc "${install_dir}" "${rollback_dir}/libexec"
	fi
	# The configuration and the downloaded profiles are what a rollback has to
	# put back; logs and the DNS cache are not, and the old logs are worth
	# leaving where they are for whoever is debugging the bad upgrade.
	if [ -d "${support_dir}/cfg" ]; then
		run ditto --rsrc "${support_dir}/cfg" "${rollback_dir}/cfg"
	fi
	if [ -d "${app_bundle}" ]; then
		run ditto --rsrc "${app_bundle}" "${rollback_dir}/Jorbique VPN.app"
	fi
	if [ -f "${daemon_plist}" ]; then
		run cp -p "${daemon_plist}" "${rollback_dir}/daemon.plist"
	fi
	if [ -f "${bar_plist}" ]; then
		run cp -p "${bar_plist}" "${rollback_dir}/bar.plist"
	fi
	if [ "${dry_run}" -eq 1 ]; then
		printf '+ write %s (%s)\n' "${rollback_dir}/VERSION" "${version}"
	else
		printf '%s\n' "${version}" >"${rollback_dir}/VERSION"
	fi
}

# start_client brings the daemon back after the files under it changed. The
# installer's postinstall only restarts a daemon that is already loaded, and an
# upgrade booted it out on purpose before touching the binary — without this the
# machine ends an otherwise successful upgrade with no daemon at all.
start_client() {
	[ -f "${daemon_plist}" ] || [ "${dry_run}" -eq 1 ] || return 0
	local bin
	bin="$(client_binary)"
	# The package's postinstall loads the daemon itself, so by the time this
	# runs it is usually already up — and bootstrapping a loaded job prints
	# "Bootstrap failed: 5: Input/output error", which reads like something
	# broke when nothing did. Load it only when it is not there; the kickstart
	# below is what picks up the new binary either way.
	if [ "${dry_run}" -eq 1 ] || ! launchctl print "system/${daemon_label}" >/dev/null 2>&1; then
		# Through the client's own install-service — launchd.Install, the same
		# primitive the installer calls. It waits for a booted-out job to
		# actually go and retries the bootstrap launchd refuses with EIO while
		# it has not, which is precisely the case that used to end an upgrade
		# with no daemon at all. A second copy of that here by hand would be a
		# copy that stops getting the fixes.
		#
		# Bare launchctl only for a snapshot too old to have the subcommand: an
		# older build restored by a rollback still has to come back up.
		if [ -z "${bin}" ] || ! run "${bin}" install-service; then
			run launchctl bootstrap system "${daemon_plist}" || true
		fi
	fi
	run launchctl kickstart -k "system/${daemon_label}" || true
}

# health_check is what decides whether an upgrade stands or is undone, so it
# asks the questions whose answers differ between a good install and a bad one:
#
#   the binary runs           a missing file, the wrong architecture, a build
#                             that cannot start
#   launchd has the daemon    the tunnels come back on their own only if it does
#   the daemon answers        a job launchd started and that then died, or that
#                             never got as far as listening, is a loaded job and
#                             a client that can do nothing privileged — every
#                             tunnel it is asked for will fail
#   `status` answers          it reads the config and walks the same
#                             bookkeeping a start does, so a config the new
#                             version rejects or a build broken in that path is
#                             caught here rather than by the first Connect
#
# It stops short of bringing a tunnel up. Whether the far end answers is not the
# new build's fault, and undoing a release because a server was down does more
# damage than the bug it would find — which is why `status` is asked without its
# connectivity probe.
health_check() {
	[ "${dry_run}" -eq 1 ] && {
		printf '+ health check %s\n' "${install_dir}/JorbiqueVPN"
		return 0
	}
	local bin
	bin="$(client_binary)"
	[ -n "${bin}" ] || {
		log "health check: no client binary in ${install_dir}"
		return 1
	}
	if ! "${bin}" help >/dev/null 2>&1; then
		log "health check: ${bin} does not run"
		return 1
	fi
	if [ -f "${daemon_plist}" ]; then
		if ! launchctl print "system/${daemon_label}" >/dev/null 2>&1; then
			log "health check: the daemon is not loaded"
			return 1
		fi
		if ! wait_for_daemon; then
			log "health check: the daemon is loaded but not listening on ${control_socket}"
			return 1
		fi
	fi
	if ! "${bin}" status --no-connectivity >/dev/null 2>&1; then
		log "health check: the client cannot read its config or report its tunnels"
		return 1
	fi
	return 0
}

# wait_for_daemon gives the daemon the moment it needs to bind its socket.
# `launchctl kickstart` returns as soon as the process is spawned, so asking
# straight afterwards would fail against a perfectly good build; a build that is
# actually broken never binds and this costs it the whole budget once.
wait_for_daemon() {
	local i=0
	while [ "${i}" -lt 25 ]; do
		[ -S "${control_socket}" ] && return 0
		sleep 0.2
		i=$((i + 1))
	done
	return 1
}

install_pkg() {
	local pkg="$1"
	[ -f "${pkg}" ] || die "no such package: ${pkg}"
	run installer -pkg "${pkg}" -target /
}

cmd_install() {
	local pkg="${1:-}"
	[ -n "${pkg}" ] || die "usage: $0 install <pkg>"
	need_root install
	take_lock
	install_pkg "${pkg}"
	start_client
	health_check || die "the installed client is not healthy; nothing to roll back to"
	log "installed $(pkg_version "${pkg}")"
}

cmd_upgrade() {
	local pkg="${1:-}"
	[ -n "${pkg}" ] || die "usage: $0 upgrade <pkg>"
	[ -f "${pkg}" ] || die "no such package: ${pkg}"
	need_root upgrade
	take_lock
	local from to
	from="$(installed_version || true)"
	to="$(pkg_version "${pkg}")"
	if [ -z "${from}" ] && [ -n "$(client_binary)" ]; then
		# Rolled back: the receipt was forgotten with the version it named, but
		# the client itself is there and can be upgraded again. The snapshot
		# still knows which version it is.
		from="$(snapshot_version || true)"
		[ -n "${from}" ] || from="unknown"
	fi
	if [ -z "${from}" ]; then
		# A plan is worth printing even on a machine that has never had the
		# client: that is how the steps get reviewed before anyone runs them.
		[ "${dry_run}" -eq 1 ] || die "nothing installed yet — use: $0 install ${pkg}"
		from="<not installed>"
	fi
	log "upgrading ${from} → ${to:-?}"

	stop_client
	take_snapshot "${from}"
	# An installer that fails leaves the machine somewhere between the two
	# builds, which is the case the snapshot was taken for. Without the `if` the
	# script would stop right here on its own errexit and leave it there.
	if ! install_pkg "${pkg}"; then
		log "the installer failed — rolling back to ${from}"
		undo_upgrade "${from}"
	fi
	start_client

	if health_check; then
		log "upgraded to ${to:-?}; roll back with: $0 rollback"
		return 0
	fi
	log "the new client is not healthy — rolling back to ${from}"
	undo_upgrade "${from}"
}

# install_root restores a file the system reads as root-owned. Ownership is only
# set when there is a root to set it to — on a machine there always is, since
# every command that gets here demanded root first.
install_root() {
	local mode="$1" src="$2" dst="$3"
	if [ "${dry_run}" -eq 0 ] && [ "$(id -u)" -ne 0 ]; then
		try install -m "${mode}" "${src}" "${dst}"
		return
	fi
	try install -m "${mode}" -o root -g wheel "${src}" "${dst}"
}

# restore_snapshot puts the previous build back, and returns non-zero if any of
# it did not work. Its steps go through `try` rather than `run`: see the comment
# there for why this is the one place that must not stop at the first failure.
restore_snapshot() {
	[ -d "${rollback_dir}/libexec" ] || die "no snapshot in ${rollback_dir}"
	restore_failed=0
	stop_client
	try rm -rf "${install_dir}"
	try ditto --rsrc "${rollback_dir}/libexec" "${install_dir}"
	if [ -d "${rollback_dir}/cfg" ]; then
		try ditto --rsrc "${rollback_dir}/cfg" "${support_dir}/cfg"
	fi
	if [ -d "${rollback_dir}/Jorbique VPN.app" ]; then
		try rm -rf "${app_bundle}"
		try ditto --rsrc "${rollback_dir}/Jorbique VPN.app" "${app_bundle}"
	fi
	if [ -f "${rollback_dir}/daemon.plist" ]; then
		install_root 644 "${rollback_dir}/daemon.plist" "${daemon_plist}"
	fi
	if [ -f "${rollback_dir}/bar.plist" ]; then
		try install -m 644 "${rollback_dir}/bar.plist" "${bar_plist}"
	fi
	# The daemon is not brought back here: both callers run start_client
	# immediately afterwards, and that is where the one way of loading it lives.
	# The receipt still names the version that failed; forget it so `status` and
	# any later upgrade compare against what is actually on disk.
	run pkgutil --forget "${pkg_id}" || true
	return "${restore_failed}"
}

# undo_upgrade is the failure path of an upgrade, and the only thing standing
# between the machine and no working client. Whatever the restore does, the
# daemon is started again afterwards and the message says which of the two
# situations the reader is in: back on the old build, or stranded between them
# and needing a .pkg. Calling restore_snapshot bare would let errexit end the
# script inside it, with no daemon and no explanation.
undo_upgrade() {
	local from="$1" rc=0
	restore_snapshot || rc=$?
	start_client
	if [ "${rc}" -ne 0 ]; then
		die "upgrade failed and the rollback to ${from} did not finish — this machine is between builds; reinstall from a .pkg"
	fi
	die "upgrade failed, ${from} restored"
}

cmd_rollback() {
	need_root rollback
	take_lock
	local snap
	snap="$(snapshot_version || true)"
	[ -n "${snap}" ] || die "no snapshot to roll back to"
	log "rolling back to ${snap}"
	local rc=0
	restore_snapshot || rc=$?
	start_client
	[ "${rc}" -eq 0 ] || die "the rollback to ${snap} did not finish — this machine is between builds; reinstall from a .pkg"
	health_check || die "the restored client is not healthy either — reinstall from a .pkg"
	log "rolled back to ${snap}"
}

cmd_uninstall() {
	need_root uninstall
	take_lock
	stop_client
	run launchctl bootout "gui/$(id -u)/${bar_label}" || true
	run rm -f "/Library/LaunchAgents/${bar_label}.plist"
	run rm -f "${HOME}/Library/LaunchAgents/${bar_label}.plist"
	run rm -f "${daemon_plist}"
	run rm -rf "${app_bundle}"
	run rm -rf "${install_dir}"
	run rm -f "${bin_dir}/jorbique-vpn" "${bin_dir}/jorbique-vpn-admin"
	run pkgutil --forget "${pkg_id}" || true
	if [ "${purge}" -eq 1 ]; then
		# The configuration, the downloaded profiles with their private keys, the
		# logs and the caches.
		run rm -rf "${support_dir}"
		run rm -rf "${logs_dir}"
		run rm -rf "${caches_dir}"
		run rm -rf "${prefix}/var/run/jorbique-vpn"
		# The per-user half: the menu bar's log and its run directory. Only for
		# the account running this — another user's data is theirs to remove.
		run rm -rf "${HOME}/Library/Application Support/JorbiqueVPN"
		run rm -rf "${HOME}/Library/Logs/JorbiqueVPN"
		log "removed the client and its data (other accounts keep their own ~/Library files)"
	else
		log "removed the client; data kept in ${support_dir} (add --purge to drop it)"
	fi
}

usage() {
	sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
}

command=""
args=()
while [ $# -gt 0 ]; do
	case "$1" in
	--dry-run) dry_run=1 ;;
	--purge) purge=1 ;;
	-h | --help)
		usage
		exit 0
		;;
	-*) die "unknown option: $1" ;;
	*)
		if [ -z "${command}" ]; then command="$1"; else args+=("$1"); fi
		;;
	esac
	shift
done

case "${command}" in
status) cmd_status ;;
install) cmd_install "${args[@]:-}" ;;
upgrade) cmd_upgrade "${args[@]:-}" ;;
rollback) cmd_rollback ;;
uninstall) cmd_uninstall ;;
"")
	usage
	exit 64
	;;
*) die "unknown command: ${command}" ;;
esac
