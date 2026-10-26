# The naive outbound and the cronet library

`naive` is a naiveproxy client — Chromium's network stack. sing-box does not
implement it: the Windows build loads it at runtime from `libcronet.dll`, which
the sing-box download does not carry. The update server publishes that library
beside the sing-box archive, and the client installs it next to sing-box.exe.

The macOS build has naiveproxy in it natively and loads nothing. The Extended channel builds macOS engines natively with `with_naive_outbound`; a Linux cross-build cannot provide the Chromium integration.

## Automatic priority

On every Connect, both clients identify NaiveProxy by its outbound type and test it before any other protocol. A successful pair of 64-KiB HTTPS transfers keeps only that outbound. If Naive cannot start or carry the payload, the client scans the other protocols and chooses the fastest successful one. There is no manual protocol or mode control in the UI. The old `naive_mode` store field and four-line `set-singbox-config` CLI format remain readable for older integrations; neither overrides automatic connections.

Windows installs the matching Cronet DLL before testing. The private probe executable has a different name from `sing-box.exe`, so tunnel recovery cannot mistake it for a running VPN; the DLL is linked or copied beside it too. macOS checks the installed build's feature tags and can refresh the channel binary. An unavailable optional transport is excluded instead of blocking all other protocols.

## Where the library goes, and under what name

`update.EnsureCronet` downloads the file `version.json` names, verifies it
against the sha256 `version.json` states, and installs it **next to
sing-box.exe** — the directory the client already downloads the binary into —
under the name `libcronet.dll`.

Both details are load-bearing. cronet-go's Windows loader
(`internal/cronet/loader_windows.go`) looks for a file called exactly
`libcronet.dll` in the directory of the running executable, then on `$PATH`.
Keeping the published name, or putting the file anywhere else, means sing-box
does not find it.

## Which build — the server publishes it

`version.json` names it, in a `naive` block beside the sing-box assets:

```json
"naive": {
  "platforms": ["windows-amd64"],
  "library_filename": "libcronet.dll",
  "cronet_version": "154.0.8037.49",
  "url": "https://<host>/<channel>/libcronet-windows-amd64.dll",
  "sha256": "8b005a933ac0d0899c047617a9cd18418810d9426d0919c824520094a2c893f5"
}
```

sing-box and cronet share an ABI: symbols are looked up by name, so a library
from another build fails at start. The server's build therefore takes the
library from the very cronet-go Go module the Windows sing-box was compiled
against (`lib/windows_amd64` carries `libcronet.dll` beside the version
constant), and sing-box's `go.sum` vouches for that module. cronet-go's GitHub
releases are not a substitute: they trail the module, and sing-box
1.15.0-alpha.10 pins 154.0.8037.49, which has no release at all — the client
used to search the releases and found nothing.

- `platforms` are manifest asset keys. A client whose `<goos>-<goarch>` is not
  listed installs nothing — that is how the manifest states macOS needs no
  library. A block with no list applies to Windows, the only sing-box here that
  loads one.
- `url` and `sha256` are the file and its checksum. The download is held to the
  sing-box binary's rules: https to any real host, and nothing reaches the disk
  that does not match the checksum. The manifest arrives over the pinned
  channel, so the checksum is the server's own word for the file.
- `library_filename` must be a bare filename. It names a file in the sing-box
  directory, not a path out of it.
- `cronet_version` is only shown in the log.

**Without a block**, or without a `url`, Windows has nothing to fetch: the
client logs that a profile using `naive` will not start, and keeps a library
that is already installed.

`libcronet.version` beside the library records the sha256 of the installed
file, so it stops being re-checked once it is on disk, and a new library on the
server — a new sing-box, in either direction — replaces it. The server being
unreachable is not fatal while a library is already installed.

The download is driven by the manifest, not by what the profile offers, so a
Windows client fetches the library once even while the profile serves no
`naive` outbound. It is ~10 MB, and it is what makes adding `naive`
server-side take effect without a client release.

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
