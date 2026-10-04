# Who runs as what (macOS)

The menu bar — the application the user sees — runs as the logged-in user and
never as root. Starting a tunnel, setting DNS on an interface and installing
launchd jobs do need root, and all of them happen in
one place: the daemon.

## The parts

| Component | Runs as | Installed at |
|---|---|---|
| Menu bar (`--menu-bar`) | the logged-in user, from a LaunchAgent | `~/Library/LaunchAgents/one.jorbique.vpn.bar.plist` |
| Daemon (`--daemon`) | root, from a LaunchDaemon | `/Library/LaunchDaemons/one.jorbique.vpn.plist` |
| Client binary | whoever runs it | `/usr/local/libexec/jorbique-vpn/JorbiqueVPN` |
| sing-box | root, started by the daemon | `/usr/local/libexec/jorbique-vpn` |

The daemon is installed with the client, not when someone asks for boot
autostart. It is the component that holds root, so the menu bar depends on it
being there. Whether tunnels come back **at boot** is a separate choice,
recorded in `/Library/Application Support/JorbiqueVPN/boot-autostart` and set
from the menu ("Start VPNs at boot"). Without that marker the daemon serves
requests and leaves the tunnels alone: nothing comes up at boot, and its
watchers return without acting, so the menu bar's own healing is the only one
running. The marker is read on every event, so switching it takes effect at
once — the daemon is not restarted for it, and cannot be: the request that
flips it is one the daemon itself is serving.

## Launching the app

Double-clicking the app starts the menu bar as the logged-in user, with no
password prompt: the package installed the daemon, and the bar needs nothing
else. The prompt appears only on a machine where the daemon is missing —
someone copied the .app in by hand, or removed the LaunchDaemon — and then it
runs once, to put the service back.

## How the menu bar gets something done

It runs the client binary as the user. A subcommand that needs root connects to
the daemon's socket and asks:

```
/var/run/jorbique-vpn/control.sock   root:admin, 0660
```

The daemon accepts one operation from a fixed list — start, stop, restart,
update-singbox, clear-singbox-cache, set-route-rules, set-singbox-config,
refresh-profile, install-autostart, uninstall-autostart — with arguments that
have to be VPN names, or a literal flag the named command reads. The request
never names a program, a path or a file, and the daemon runs the operation as a
child of itself, streaming back output and the exit status.

Refused: anything not on that list, an argument containing a slash, `..`, a
leading dash, a newline or a null, an argument longer than 64 characters, more
than 16 arguments, a payload over 64 KB.

The socket is the privilege boundary. It is root-owned and open to the admin
group — the same people who could run privileged subcommands before, and nobody
else.

## File ownership

| Path | Owner | Mode |
|---|---|---|
| `/usr/local/libexec/jorbique-vpn` | `root:wheel` | 0755 |
| `/Library/Application Support/JorbiqueVPN` | `root:wheel` | 0755 |
| `…/JorbiqueVPN/cfg` (config, profile, user UUID) | `root:admin` | 0750, files 0640 |
| `/Library/Logs/JorbiqueVPN` (daemon, tunnels) | `root:wheel` | 0755 |
| `/Library/Caches/JorbiqueVPN` | `root:wheel` | 0755 |
| `~/Library/Logs/JorbiqueVPN` (menu bar) | the user | 0755 |
| `~/Library/Application Support/JorbiqueVPN/run` | the user | 0755 |

Nothing is group-writable: the privileged engine reads the configuration. The installer sets this table and every privileged start
repairs it, so an upgraded machine converges without being reinstalled.

## What is still open

The task asks for the privileged component to live in
`/Library/PrivilegedHelperTools` and be managed by `SMAppService`. Both are
blocked on one thing: this build is ad-hoc signed (`TeamIdentifier` not set),
and every supported way of installing a privileged helper checks the signature
before it will bless anything.

### What the certificate is, and what it costs

$99/year, Apple Developer Program. Organization enrollment needs a D-U-N-S
number and a legal-entity check, which is normally the part that takes weeks —
we have neither ahead of us: the D-U-N-S number exists, and the company is
already set up in Apple Business Manager, so Apple has verified the entity.
What is left is the enrollment itself and the payment. Apple Business Manager
issues no certificates and signs nothing; the Developer Program membership is
separate, on the same legal entity. Membership issues two certificates that
matter here:

| Certificate | Signs |
|---|---|
| Developer ID Application | the `.app`, the client binary, the helper |
| Developer ID Installer | the `.pkg` |

There is nothing else to buy. The remaining work is CI: the certificate and its
private key have to reach a keychain on the runner, and notarization needs an
App Store Connect API key or an app-specific password.

### Two different APIs, and they install to different places

They are often quoted together, and the task names both, but they are not the
same mechanism:

- **`SMAppService`** (macOS 13+) registers a daemon whose plist ships *inside
  the app bundle*, at `Contents/Library/LaunchDaemons/<label>.plist`. It does
  not install anything into `/Library/PrivilegedHelperTools`. The app has to be
  signed and notarized, and the user approves the daemon once in System
  Settings → General → Login Items & Extensions. `register()`, `unregister()`
  and `status` replace `launchctl bootstrap`, `bootout` and `print`.
- **`SMJobBless`** is the API that puts a helper in
  `/Library/PrivilegedHelperTools`. It is the older one, and it is the one that
  wants matching code requirements: the helper's Info.plist declares
  `SMAuthorizedClients`, the app's declares `SMPrivilegedExecutables`, and both
  requirements pin the Team ID, e.g.

  ```
  identifier "one.jorbique.vpn" and anchor apple generic and
    certificate leaf[subject.OU] = "<TeamID>"
  ```

Either way the signature is the gate, so the certificate unblocks both and we
choose afterwards. `SMAppService` is the one Apple documents for new code.

### What else the certificate changes

Notarization stops being optional the moment the client is downloaded rather
than built locally: Gatekeeper blocks an unnotarized `.pkg` on first open.
Notarizing requires the Hardened Runtime at signing time, which in turn needs
the entitlements the tunnels want declared rather than assumed. That is a build
change, not a code change, but it lands with the same certificate.

`SMAppService.loginItem` would also replace the menu bar's hand-written
LaunchAgent plist, and with it the `launchctl bootstrap`/`bootout` pair in
`internal/macos/launchd/barlogin.go`.

### What does not change

The socket protocol. The daemon keeps the same fixed operation list, the same
argument validation and the same root:admin socket — only how it gets installed
and started changes. Nothing in `internal/macos/control` moves.
