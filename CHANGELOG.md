# Changelog

## v0.1.0 — 2026-11-19

- `proctop.top[<n>,<cpu|mem>,<text|json>]`: top processes by CPU or memory.
- `proctop.net.conns[<n>,<text|json>]`: top processes by established TCP connections.
- `proctop.net.listen[<text|json>]`: exposed ports and their process.
- Options `ListenExcludeWindowsRPC` and `ListenExcludePorts`.
- Builds for Zabbix agent 2 7.4 and 8.0, on Windows amd64 and Linux amd64, arm64 and armv7.
