# Automatic connections

Each Connect starts a fresh check before creating the TUN interface:

1. Test NaiveProxy first. If it completes two trusted HTTPS transfers, use it alone.
2. Otherwise test each remaining server-provided protocol with the same two 64-KiB payloads, through an isolated loopback SOCKS proxy with remote DNS.
3. Keep successful candidates and choose the lowest total transfer time as the primary connection. An open port, authentication refusal, truncated response or 16-KiB stall does not count as success.
4. Apply that winner after engine startup too, overriding any selector restored from its persistent cache. If every check fails, leave the VPN disconnected.

The animated connection card shows actual stages and finished-check counts. Tests are sequential, bounded to 10 seconds per candidate and 75 seconds for the scan, and download at most 128 KiB per successful candidate. The common measurement target is `https://speed.cloudflare.com/__down?bytes=65536`; this is a short comparison of current paths, not a sustained bandwidth benchmark. The UI offers no protocol switches. Historical manual selections and unsaved account drafts do not override a new scan; the existing saved UUID, app/site exceptions and autostart settings survive an upgrade.
