# Разработка

Подробности для тех, кто собирает клиент сам. Пользователю достаточно
[README](../README.md).

Personal VPN client for **Windows** and **macOS**, built on sing-box.

The client downloads its profile from the server and runs only the profile's
proxy outbounds (VLESS/Reality RMUX, VLESS/XHTTP, Mieru, Sudoku and AnyTLS), so
every connection changes the external IP. `endpoints` (WireGuard, OpenVPN,
Tailscale) are dropped from the profile, together with every DNS server and
route rule that pointed at them.

## Server

| What | URL |
|------|-----|
| Client profile | `https://jorbique.one/chI6dwCVntNE-extended-v2/singbox_client.json` |
| sing-box manifest | `https://jorbique.one/chI6dwCVntNE-extended-v2/version.json` |
| UUID check and metrics (OTLP) | `https://jorbique.one/otlp/v1/metrics` |

The profile is shared by every user: it carries no user UUID. The UUID entered
on first launch is checked against the server. The server marks the exact UUID
fields with `__JORBIQUE_USER_UUID__`; the client replaces only these markers in
the new profile. Sudoku receives its server key and TLS settings unchanged;
its marker is the HTTPMask path authorized by nginx. Legacy profiles still
support empty top-level credential fields. NaiveProxy is tested first on every Connect and used alone when it works. Otherwise the fastest working protocol is selected. See [automatic connection checks](automatic-connections.md). nginx serves one of eight
profile variants per request (`singbox_client.r0.json` … `r7.json`), each with
its own Reality `short_id`.

The host is a build-time variable: `-X jorbiquevpn/internal/constants.ServerHost=<host>`
(`SERVER_HOST=<host>` for `scripts/build-windows.sh`).

New app builds use the separate `chI6dwCVntNE-extended-v2` channel for
[sing-box-extended](https://github.com/shtorm-7/sing-box-extended). Installed
older apps keep their original `chI6dwCVntNE` URLs, profile, and SagerNet
`1.15.0-alpha.12` engine; deploying the extended server does not switch their
engine channel. Version 1.3.0 keeps its existing `chI6dwCVntNE-extended` channel too. Publish the new channel before releasing the new app.

Upgrading the app opts that installation into the extended channel. The new
app can migrate its installed SagerNet engine even when the fork's upstream
version is lower, such as `1.15.0-alpha.12` to `1.14.3-extended-2.7.3`. After
migration, automatic updates accept newer extended builds and keep the full
fork version.

---

## Windows

Build (on Windows: `build-windows.bat`; elsewhere: `scripts/build-windows.sh`),
then:

1. Extract **Jorbique-VPN-Extended-windows.zip**.
2. Run **Jorbique-VPN-Extended.exe** with administrator privileges.
3. Enter your user UUID once, then use **Connect** / **Disconnect**. **Set UUID** changes the saved account; protocol selection is automatic.

The tray icon exposes the same actions as the main window. sing-box is
downloaded from the server on first launch.

---

## macOS

### Build and install

```bash
./build-macos.sh package   # Jorbique-VPN-Extended-macOS.pkg
./build-macos.sh install   # build and install on this Mac
```

1. Open **Jorbique-VPN-Extended-macOS.pkg** and install it.
2. **Jorbique VPN Extended.app** appears in **Applications**.
3. Launch the app and enter your user UUID. The package installs the privileged daemon; an administrator prompt appears only if that service needs to be restored.

The menu bar runs as the logged-in user and never as root. Operations that need
root (starting a tunnel, setting DNS, installing autostart) are
performed by the privileged daemon, which accepts a fixed set of requests over a
socket in `/var/run/jorbique-vpn`. See [docs/privileges.md](privileges.md).

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
sudo jorbique-vpn-admin upgrade Jorbique-VPN-Extended-macOS.pkg   # snapshots first, rolls itself back if the new build fails
sudo jorbique-vpn-admin rollback                        # back to the previous version
jorbique-vpn-admin status                               # what is installed, what a rollback would restore
```

See [docs/upgrade-rollback.md](upgrade-rollback.md).

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

GitHub Actions runs [the build workflow](../.github/workflows/ci.yml) for pull
requests, pushes to `main`, version tags, and manual dispatches. It checks and
builds the frontend, runs Go vet/unit/integration checks on Linux, and produces
`Jorbique-VPN-Extended-windows.zip` and `Jorbique-VPN-Extended-macOS.pkg` as downloadable workflow
artifacts. A `vX.Y.Z` tag publishes them to the release with the version in the
name (`Jorbique-VPN-Extended-windows-X.Y.Z.zip`, `Jorbique-VPN-Extended-macOS-X.Y.Z.pkg`)
plus `SHA256SUMS`, so `releases/latest/download/...` links need the exact name. Windows is cross-built on Linux; the universal macOS installer
(Apple Silicon and Intel) is built on a macOS runner, which also runs the
macOS-only CLI tests. Linux integration checks use a pinned, checksum-verified
sing-box-extended binary to validate generated configs and the local API.
For the same checks locally, set `SINGBOX_BIN` to a compatible sing-box executable;
without it, real-engine checks remain skipped.

To release, commit the new `VERSION` and push the matching `vX.Y.Z` tag. After
all checks and packages succeed, the workflow publishes a GitHub Release with
both packages and `SHA256SUMS`. Tags that do not match `VERSION` fail validation.
A rerun can finish an unpublished draft, but cannot replace a published release.
Build jobs have read-only repository access; only the release job can publish.
Version resource files are generated in the CI checkout and are not committed.

The macOS executable is ad-hoc signed, and the installer is not notarized or
Developer ID signed. Signing credentials are not required for these builds;
Gatekeeper distribution signing remains a separate configuration step.

## Tests

```bash
go test ./...
(cd frontend && npm test)
```
