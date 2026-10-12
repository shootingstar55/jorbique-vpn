# Jorbique VPN

Personal VPN client for **Windows** and **macOS**, built on sing-box.

The client downloads its profile from the server and runs only the profile's
proxy outbounds (VLESS Reality, VLESS gRPC, AnyTLS, Snell, …), so
every connection changes the external IP. `endpoints` (WireGuard, OpenVPN,
Tailscale) are dropped from the profile, together with every DNS server and
route rule that pointed at them.

## Server

| What | URL |
|------|-----|
| Client profile | `https://jorbique.one/chI6dwCVntNE/singbox_client.json` |
| sing-box manifest | `https://jorbique.one/chI6dwCVntNE/version.json` |
| UUID check and metrics (OTLP) | `https://jorbique.one/otlp/v1/metrics` |

The profile is shared by every user: it carries no user UUID. The UUID entered
on first launch is checked against the server and then filled into every empty
`uuid` / `password` field of the proxy outbounds. nginx serves one of eight
profile variants per request (`singbox_client.r0.json` … `r7.json`), each with
its own Reality `short_id`.

The host is a build-time variable: `-X jorbiquevpn/internal/constants.ServerHost=<host>`
(`SERVER_HOST=<host>` for `scripts/build-windows.sh`).

---

## Windows

Build (on Windows: `build-windows.bat`; elsewhere: `scripts/build-windows.sh`),
then:

1. Extract **Jorbique-VPN-windows.zip**.
2. Run **Jorbique-VPN.exe** with administrator privileges.
3. Enter your user UUID, then use **Connect** / **Disconnect**, protocol settings, VPN Config and Logs in the main window.

The tray icon exposes the same actions as the main window. sing-box is
downloaded from the server on first launch.

---

## macOS

### Build and install

```bash
./build-macos.sh package   # Jorbique-VPN-macOS.pkg
./build-macos.sh install   # build and install on this Mac
```

1. Open **Jorbique-VPN-macOS.pkg** and install it.
2. **Jorbique VPN.app** appears in **Applications**.
3. Launch the app and enter your user UUID. The package installs the privileged daemon; an administrator prompt appears only if that service needs to be restored.

The menu bar runs as the logged-in user and never as root. Operations that need
root (starting a tunnel, setting DNS, installing autostart) are
performed by the privileged daemon, which accepts a fixed set of requests over a
socket in `/var/run/jorbique-vpn`. See [docs/privileges.md](docs/privileges.md).

Binaries are installed under `/usr/local/libexec/jorbique-vpn/` (JorbiqueVPN,
sing-box, jorbique-vpn-admin) and reachable on the PATH as `jorbique-vpn`;
configuration and the downloaded profile live in
`/Library/Application Support/JorbiqueVPN/` (root-owned, admin-readable), daemon
and tunnel logs in `/Library/Logs/JorbiqueVPN/`, and the menu bar's own log in
`~/Library/Logs/JorbiqueVPN/`.

Every path, launchd label (`one.jorbique.vpn`, `one.jorbique.vpn.bar`) and
socket is the client's own, so it installs next to another sing-box client
without touching it.

### Upgrade and rollback

```bash
sudo jorbique-vpn-admin upgrade Jorbique-VPN-macOS.pkg   # snapshots first, rolls itself back if the new build fails
sudo jorbique-vpn-admin rollback                        # back to the previous version
jorbique-vpn-admin status                               # what is installed, what a rollback would restore
```

See [docs/upgrade-rollback.md](docs/upgrade-rollback.md).

### Autostart

**VPN autostart** (LaunchDaemon)

- Starts the background daemon at system boot.
- Rechecks VPN after sleep and network changes, then restarts tunnels if needed.
- Configure VPN autostart in the application settings.

**App autostart** (LaunchAgent, no root)

- Starts the **menu bar app** when the user logs in.
- **Disable app autostart** only disables the next login launch; the running menu bar icon stays open.

Menu bar logs: `~/Library/Logs/JorbiqueVPN/bar.err.log`

---

## Version

`VERSION` holds the version both clients build with. Bump it to release a new
version; `APP_VERSION=x.y.z` overrides it for one build.

## CI

`.gitlab-ci.yml` runs on a Linux runner (Docker executor, x86_64): frontend
checks, Go tests, and `Jorbique-VPN-windows.zip` and `Jorbique-VPN-macOS.pkg`
as job artifacts. The macOS package is cross-built there, with no Mac involved.

## Tests

```bash
go test ./...
(cd frontend && npm test)
```
