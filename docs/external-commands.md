# External commands: what runs, and why

Every place this client starts another program, what it is for, and what it was
replaced with or why it stays. Both platforms: the macOS calls first, the
Windows ones in their own section at the end.

## Removed

The client runs no `openvpn` process: sing-box is the only engine.

| What ran | Where | Replaced by |
|---|---|---|
| `/usr/bin/curl` (exit-IP checks, connectivity probes) | `ipgeo`, `vpnmgr` | `internal/webprobe` — `net/http` with curl's `--fail`, `--location`, `--max-time` and `-4` as options |
| `launchctl asuser` + `sudo -u` ladder around curl | `procutil` | gone with curl: it existed so a root menu bar could make a request as the logged-in user, and the menu bar is not root any more |
| `/usr/bin/stat -f %Su /dev/console` | `procutil`, `ui` | `os.Stat` + `syscall.Stat_t` + `user.LookupId` |
| `/usr/bin/install -m 644` (LaunchDaemon plist) | `launchd` | `os.WriteFile` + rename, which is also atomic |
| `/bin/rm -f` (plists, temp files) | `launchd` | `os.Remove` |
| `/usr/bin/env <program>` (sing-box) | `vpnmgr` | the program directly — its path was already absolute, so `env` resolved nothing |
| `networksetup -setdnsservers` / `-setsearchdomains` | `netsetup` | `applekit` → SCPreferences: find the service by name, replace one half of its DNS configuration, commit, apply. Read-modify-write, so setting resolvers no longer has to think about search domains. The tool remains as a fallback for one release |

## Kept, with reasons

| What runs | Why it is not a library call |
|---|---|
| `sing-box` (`run`, `version`, `format -w`) | **Licence first:** sing-box is GPL-3.0. Importing it as a Go package makes this client a derived work, and shipping it to a customer then carries the obligation to release the client's own source under the same terms. Running it as a separate program, which is how we ship it today, does not. Setting the licence aside, three things break: the client downloads and updates this binary — that is the feature — so a linked copy would answer for the version we built against rather than the one installed; `format -w` exists precisely to let the installed binary decide what its own version accepts; and a library-run tunnel dies with our process, where today it survives a client restart. |
| `launchctl` (bootstrap, bootout, kickstart, print, enable, asuser) | The supported API is `SMAppService`, which requires a Developer ID signature and matching `SMAuthorizedClients`; this build is ad-hoc signed, so the system would refuse to bless the helper. The certificate costs $99/year and what it buys — plus what else has to change with it — is written out in `docs/privileges.md`. |
| `networksetup` (`-setdnsservers`, `-setsearchdomains`) | **Fallback only.** The writes go through SystemConfiguration (`applekit.SetServiceDNSServers`, `SetServiceSearchDomains`) — the API the tool itself calls. The tool is kept behind it for one release: this path writes the machine's network configuration, and a client whose DNS silently stops being set looks connected and resolves nothing. Which path ran is in the log. |
| `networksetup -listnetworkserviceorder`, `-listallhardwareports`, `-getairportnetwork`, `-getinfo` | Reads. `SCNetworkSetCopyServices` covers the service list (`applekit.NetworkServiceNames`); the hardware-port and Wi-Fi-SSID reads want CoreWLAN, which is a separate binding job. |
| `dscacheutil -flushcache`, `killall -HUP mDNSResponder` | There is no API. Apple's documented way to flush the DNS cache is these two commands. |
| `plutil -convert json` (reading an app's `Info.plist`) | Info.plist is often a binary plist. Parsing it in Go means a third-party plist library; `plutil` is on every Mac and reads exactly what the system reads. |
| `osascript` (the setup dialog in the .app launcher) | The launcher is a shell script that runs before the client binary exists in the process; the dialog is AppleScript because that is what a script can present. |
| `sudo` | One call site left: a person running a privileged subcommand from a terminal on a machine whose daemon is not running. The menu bar never takes this path. |

## One place starts a process

`internal/sysexec` is the only package in the client that constructs a command.
Everything else asks it:

```go
cmd := sysexec.Command("/bin/launchctl", "bootout", domain)
```

It is `exec.Command` with two rules applied, so they hold once instead of being
restated as a comment beside each call:

- **No shell, ever.** A shell — or `env`, which picks the program for us — is
  refused. Arguments are passed as a slice and nothing in them is expanded,
  quoted or interpreted.
- **Absolute program paths.** A bare name is resolved through `PATH`, which the
  client does not control, so the caller has to say which binary it means.

A refused command is not a panic and not an extra error return: the returned
`*exec.Cmd` carries the reason in its `Err` field, which is what `Start`, `Run`
and `Output` already report when a program will not launch. Call sites read as
they did before.

`TestSysexecIsTheOnlyChokepoint` walks the tree and fails on an `exec.Command`
anywhere outside that package, on either platform. There is no exception list to
add a file to: a new direct call is a failing test, and the way past it is to
call `sysexec`.

## Rules the remaining calls follow

- No shell, and absolute paths: enforced by `internal/sysexec`, above.
- Our own binaries come from the install layout (`core.Paths`), never from
  `PATH` lookups on user input.
- No user-controlled program names. The one place that takes a command from
  outside the process — the daemon's control socket — accepts an operation from
  a fixed list and arguments matched against a strict pattern; it never names a
  program. See `internal/macos/control`.
- Bounded: the synchronous sing-box calls use `sysexec.CommandContext` with a
  timeout. Tunnel processes are deliberately unbounded — they are the tunnel.
- Exit status and stderr are captured and reported, not discarded.

## `// nosemgrep`

Two, both in `internal/sysexec`, on the two lines that call `os/exec` — the
program they are given is checked on the next line. The per-call-site
suppressions went with the call sites: nothing else names `exec.Command` any
more, so nothing else has anything to suppress.

## Windows

Done. The Windows client starts no shell: `powershell`, `route`, `schtasks`,
`taskkill` and `net` are gone, and `sing-box` — the one program left — goes
through `sysexec` like every macOS call above.

| What ran | Where | Replaced by |
|---|---|---|
| `route add` / `route delete` | `internal/network/network.go` | `InitializeIpForwardEntry` + `CreateIpForwardEntry2` / `DeleteIpForwardEntry2` (iphlpapi). An "already exists" result counts as success, which is what reading `route`'s output amounted to |
| `powershell` — `Get-NetAdapter` (has the TUN adapter appeared, or gone) | `internal/network/tun_cleanup.go` | `GetAdaptersAddresses`. It also sees an adapter the cmdlet could miss: a just-orphaned one, which is the case this check exists for |
| `powershell` — NRPT rules (`Add-`, `Get-`, `Remove-DnsClientNrptRule`) | `internal/network/nrpt_dns.go` | The NRPT is registry state: one subkey per rule under `Dnscache\Parameters\DnsPolicyConfig`, the same place the cmdlet writes, then a `DnsFlushResolverCache`. The 15-second budget that batch of cmdlets needed went with it |
| `taskkill /F /PID`, `/F /IM` | `internal/core/engine.go`, `internal/network/tun_cleanup.go` | `OpenProcess` + `TerminateProcess`, with `CreateToolhelp32Snapshot` first when killing by name — the calls `taskkill` itself makes. A process that is already gone is not an error |
| `schtasks` | `internal/tray/autostart.go` | The Task Scheduler COM service (`Schedule.Service`), which `schtasks.exe` is itself a client of. It takes the task XML as a string, so the temp file, its UTF-16 encoding, and the second "build the task from flags instead" code path are gone |
| `powershell` — `Get-Process` plus `Add-Type`/`New-Object` for the icons | `internal/utils/process_windows.go` | `EnumWindows` + `QueryFullProcessImageName` for the windowed processes, `ExtractIconEx` + `DrawIconEx` into a DIB section for the icons |
| `powershell` — `Start-Process -Verb RunAs` | `cmd/jorbiquevpn` | `ShellExecute` with the `runas` verb, and `ComposeCommandLine` for the arguments — so the apostrophe-doubling PowerShell quoting required is gone with it |
| `powershell` — `[System.Windows.MessageBox]::Show` | `cmd/jorbiquevpn` | `MessageBoxW` |
| `net session` (the admin check) | `cmd/jorbiquevpn` | `Token.IsElevated()`: the token's own answer, instead of a command that answered by failing |
| `powershell` — `Add-MpPreference -ExclusionPath` | `internal/defender` | WMI: the `Add` method on `MSFT_MpPreference` in `root\Microsoft\Windows\Defender`, which is what the cmdlet calls. The script's admin-and-cmdlet probe went too — without an elevated token the call simply fails, and the caller has always ignored the result |
| `sing-box` (`run`, `check`, `version`) | `internal/core`, `internal/app` | Stays a separate program, for the reasons in the table above — through `sysexec` now |

Two are trades rather than wins. The Defender exclusion is a WMI conversation in
place of one cmdlet — more code, not less, taken for the same reason as the
rest. The process list with icons is the longest replacement here, because
drawing an `HICON` into a bitmap is a handful of GDI calls where PowerShell had
a one-liner; it is also the one that removed a visible stall from the UI, since
every one of these calls used to cost a PowerShell launch of a few hundred
milliseconds before it did any work.
