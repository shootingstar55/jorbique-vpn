# The outbound list comes from the server

Which proxy outbounds exist — `vless-reality`, `tuic`, whatever replaces them —
is the sing-box profile's answer, not the client's. Both clients read the tags
out of the profile they download, so adding, renaming or removing an outbound
server-side takes effect on the next launch with no client release.

## What counts as selectable

Every outbound in the profile **except the plumbing**: `urltest`, `selector`,
`direct`, `block`, `dns` by type, and the conventional tags `auto`, `direct`,
`block`, `dns-out`. Everything else is offered to the user by its **tag** —
including a protocol this client has never heard of, since nothing is matched
against a list of known protocol names.

Order comes from the profile too: the `urltest`'s own `outbounds` list first
(that is the server stating its preference), then anything selectable it did
not mention, in the order the profile lists them.

## What happens to a user's choices

What is remembered is what the user turned **off** — `disabled_outbounds` on
macOS, the `false` entries of the toggle map on Windows. A stored list of what
is *on* cannot tell "the user refused this" from "this did not exist when they
chose", which is the difference between an outbound the server adds later
working and staying silently off.

| Situation | Result |
|---|---|
| Nothing chosen (fresh install, never opened the panel) | everything the profile offers |
| An outbound the user unticked | stays off, across restarts and profile updates |
| An outbound the server added | on, unless the user turns it off |
| An outbound the server dropped | gone from the window and from the config |
| Every outbound the user chose is gone (a rename) | everything the profile offers, and the log names what disappeared |
| A configuration written before this existed | its `enabled_outbounds` list is read as it always was |
| No profile downloaded yet | the settings window says so; the connect path fetches one first |
| The profile changes while the settings window is open | Apply saves what is ticked and still offered; an outbound added meanwhile gets the answer the window would have shown for it |

A rename on the server therefore costs nobody their connection. Refusing to
start there would mean one server-side change bricks every client until each
user reopens a menu.

## Credentials

The user's UUID goes into every top-level field the profile left empty in an
outbound the user can pick, rather than a field decided from the protocol name:
`uuid` for VLESS and TUIC, `password` for Hysteria2 and AnyTLS, `userkey` for
Snell. A profile that adds a protocol works with no client change. A field the
server filled is left alone — TUIC ships a shared `password` beside the per-user
`uuid`, and only the `uuid` is empty. Both clients use
`singboxcfg.ApplyCredential` for this. On Windows a profile with nowhere at all
to put the credential is reported, because a config that cannot authenticate is
worse than an error.

A transport the proxies only `detour` through — a shared TLS or mux layer — is
not offered and keeps what the server gave it. It becomes a choice only when a
group the traffic can reach names it: `auto` (or the first `urltest`), or a
group that `route.final` or a route rule leads to. A leftover group nothing
routes to does not count: taking it at its word would put the user's UUID into
a transport every proxy detouring through it shares. Both clients pick the
credential's targets from the profile before filtering, because once the
proxies detouring through a transport are off, read afresh it looks like a
proxy of its own.

Such a transport stays in the build while anything still names it: a proxy
detouring through it, a group, a route rule, a DNS server's `detour` or a rule
set's `download_detour`. A `download_detour` naming something the build took
out is dropped, and that rule set downloads through the default outbound.

## Profile mistakes

A group that names itself or an ancestor loses that back edge — sing-box cannot
start on a cycle, and failing the build would stop every user over a group
nobody may use. Two outbounds sharing a tag, or a `detour` naming a tag the
profile does not have, fail the build with an error naming the tag.

## Where this lives

| | macOS | Windows |
|---|---|---|
| Tags from a profile | `singbox.ProxyTagsFromOutbounds` | `config.ProxyTagsFromConfig` |
| Cached profile | `singbox.AvailableProxyTags` | `config.AvailableProxyTags` |
| Applied when building | `singbox.BuildConfigFromRemoteTemplate` | `config.filterOutboundsAndUrltests` |
| Shown to the user | the sing-box panel in the menu bar | the VPN Configuration window (`GetAvailableOutbounds`) |

Nothing in either client names an outbound tag any more. The one exception is
cosmetic: the Windows window has prettier labels for the five tags served
today (`VLESS Reality` rather than `vless-reality`), and shows anything else by
its tag.
