# The naive outbound and the cronet library

`naive` is a naiveproxy client — Chromium's network stack. sing-box does not
implement it: the Windows build loads it at runtime from a library published by
[cronet-go](https://github.com/SagerNet/cronet-go/releases), which ships with
neither sing-box nor this client. So a Windows client that is offered a `naive`
outbound has to fetch that library itself.

The macOS build has naiveproxy in it natively and loads nothing. The channel
serves the official SagerNet macOS binaries for that reason: they are built on
macOS with `with_naive_outbound`, which a cross-build from Linux cannot do.

## A mode of its own

naive is not one more toggle. It is the heaviest protocol, and in the automatic
selector it would run beside every other protocol. The server publishes it in
the one profile, `singbox_client.json`, beside the others but outside `auto`,
and the client picks a mode in VPN Config:

- **Protocols** builds with the user's toggles and never starts naive, whatever
  its toggle would say; the settings window does not offer one.
- **NaiveProxy** builds from the profile's naive outbound alone; the toggles are
  not consulted, and `auto` is left naming naive only.

naive is found by its outbound type, not its tag. Both clients apply the mode in
one place, `singboxcfg.ModeEnabled`, before `FilterOutbounds`. The mode is
`naive_mode` in `vpn_cfg.json` on macOS (`set-singbox-config`'s fourth stdin
line: `naive`, `standard`, or blank to keep it) and in the Windows store.

On macOS the NaiveProxy mode checks the installed sing-box's `Tags:` line for
`with_naive_outbound` before it starts. A machine can hold a channel build from
before the switch to the official binaries, and an update only runs when the
version changes; the mode downloads the channel's build once, and refuses to
start with a reason if that one cannot run naive either.

## Where the library goes, and under what name

`update.EnsureCronet` downloads the asset, verifies it against the sha256 GitHub
publishes in the release metadata, and installs it **next to sing-box.exe** —
the directory the client already downloads the binary into — under the name
`libcronet.dll`.

Both details are load-bearing. cronet-go's Windows loader
(`internal/cronet/loader_windows.go`) looks for a file called exactly
`libcronet.dll` in the directory of the running executable, then on `$PATH`.
Keeping the published per-arch name, or putting the file anywhere else, means
sing-box does not find it.

## Which build — the manifest decides

`version.json` names it, in a `naive` block beside the sing-box assets:

```json
"naive": {
  "platforms": ["windows-amd64"],
  "library_filename": "libcronet.dll",
  "cronet_version": "150.0.7871.63",
  "source_repo": "https://github.com/sagernet/cronet-go",
  "source_asset": "libcronet-windows-amd64.dll"
}
```

sing-box and cronet share an ABI, so the pair tested together is the pair that
should be installed. Tracking upstream's latest means a cronet release reaches
the fleet the day it is published, against a sing-box nobody has tried it with.

- `platforms` are manifest asset keys. A client whose `<goos>-<goarch>` is not
  listed installs nothing — that is how the manifest states macOS needs no
  library. A block that exists and omits a platform is an answer, not a gap, so
  it is not overridden by the fallback below.
- `cronet_version` is the Chromium version, not a tag. Upstream's tags carry a
  build suffix and have changed shape three times (`v150.0.7871.63-1`,
  `143.0.7499.109-2`, `v0.0.1-143.0.7499.109-1`), so the version is matched as a
  whole hyphen-delimited component of the tag. Prefix matching would accept
  `150.0.7871.630` for `150.0.7871.63`.
- `source_repo` must be a `github.com/<owner>/<repo>` URL. It decides where a
  library is downloaded from and loaded into the process that runs the tunnel,
  so anything else is refused even though the manifest arrives over the pinned
  channel.
- `library_filename` must be a bare filename. It names a file in the sing-box
  directory, not a path out of it.

**Without the block** the client falls back to upstream's latest release, which
is what it did before the manifest could say. A server that has not published
the block yet must not leave Windows with no library at all.

`libcronet.version` beside the library records which release is installed, so a
pinned build stops being re-checked once it is on disk, and moving the pin — in
either direction, an upgrade or a rollback — replaces it. A GitHub outage is not
fatal while a library is already installed.

The download is driven by the manifest, not by what the profile offers, so a
Windows client fetches the library once even while the profile serves no `naive`
outbound. It is ~9 MB, and it is what makes adding `naive` server-side take
effect without a client release.

## Credentials

naiveproxy authenticates with a `username` and a `password`. The server signs
users in by their UUID as the username and one password shared by everyone, so
the profile ships `username` empty and `password` filled. The existing rule —
the UUID goes into every top-level field the profile left empty — puts the UUID
into the username and leaves the password alone, with no client code that names
naive. `TestBuildRuntimeConfig_NaiveTakesTheUUIDInItsEmptyFields` and
`TestNaiveModeBuildsFromTheNaiveProfile` pin it.

## If the profile is served from somewhere else

`internal/httpsclient` pins the SPKI chain of one host, the one both
`SingboxManifestURL` and `SingboxClientURL` point at. A profile pointed at a
staging host — which is how this outbound was tested — is fetched against the
system trust store alone, and `TestProfileIsOnThePinnedHost` exists to make that
a deliberate change rather than a quiet one.

Two things to know when moving a machine back afterwards:

- **The cache is shared.** The downloaded profile is written to the same path
  whatever it was fetched from (`%TEMP%\JorbiqueVPN\singbox_client.json` on
  Windows, next to the config on macOS), so a machine that ran a test build
  still has that profile cached, and a client falls back to the cache when the
  server cannot be reached. Delete it when switching a machine back.
- **The library stays.** `libcronet.dll` is harmless with a profile that offers
  no `naive`; sing-box only loads it for an outbound that needs it.
