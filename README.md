# ProcTop — top processes plugin for Zabbix agent 2

An unofficial, MIT-licensed loadable plugin for Zabbix agent 2 (Windows and Linux)
that answers the question every CPU or memory alert raises: **which process is it?**

```
Process             CPU
mcshield.exe     79.09%
mfevtps.exe       1.73%
chrome.exe (12)   0.29%
Other processes   1.05%
```

Put this value in a trigger's operational data and it reaches every notification,
including those analysed by an AI or sent as push notifications.

## Item keys

| Key | Returns |
|---|---|
| `proctop.top[<n>,<cpu\|mem>,<text\|json>]` | The `n` (1–25, default 10) process names using the most CPU (% of the whole machine) or memory (private working set on Windows, RSS on Linux). Processes with the same name are added up. |
| `proctop.net.conns[<n>,<text\|json>]` | The `n` process names with the most established TCP connections to other machines, with the number of distinct remote hosts and the three main ones. |
| `proctop.net.listen[<text\|json>]` | The ports reachable from other machines: TCP sockets in LISTEN state and bound UDP sockets, with their process and addresses. Loopback addresses and the ephemeral UDP ports used by clients are excluded. The output is sorted, so an unchanged state gives an identical value. |

Text output uses aligned columns and never contains curly braces, so it can go
straight into operational data. JSON output is meant for dependent items.

### How it measures

- **Windows**: one `NtQuerySystemInformation(SystemProcessInformation)` call per
  snapshot — no `OpenProcess` per PID, so protected processes (antivirus, system
  services) are measured like any other. Sockets come from `GetExtendedTcpTable`
  and `GetExtendedUdpTable`.
- **Linux**: `/proc/[pid]/stat`, `/proc/[pid]/statm` and `/proc/net/*`, readable
  by the `zabbix` user. Without root, the owner of a socket held by another
  user's process cannot be read: such sockets are reported as
  `(unknown process)`, with correct ports and counts.
- CPU usage is measured between two snapshots at least one second apart. The keys
  share their snapshots (reused for up to 30 s), so polling several keys costs one
  walk of the process list.

## Installation

Download the binary matching your **agent series** and platform from the
[releases](https://github.com/logiserv/proctop-agent2-plugin/releases), check it
against `SHA256SUMS` (or `gh attestation verify <file> --repo logiserv/proctop-agent2-plugin`),
then declare it to the agent.

**Linux**

```sh
install -m 755 zabbix-agent2-plugin-proctop-zbx7.4-linux-amd64 /usr/libexec/zabbix/zabbix-agent2-plugin-proctop
echo "Plugins.ProcTop.System.Path=/usr/libexec/zabbix/zabbix-agent2-plugin-proctop" \
  > /etc/zabbix/zabbix_agent2.d/plugins.d/proctop.conf
systemctl restart zabbix-agent2
zabbix_agent2 -c /etc/zabbix/zabbix_agent2.conf -t 'proctop.top[10,cpu]'
```

**Windows** (PowerShell as administrator)

```powershell
$dir = "C:\Program Files\Zabbix Agent 2"
New-Item -ItemType Directory -Force "$dir\plugins" | Out-Null
Copy-Item .\zabbix-agent2-plugin-proctop-zbx7.4-windows-amd64.exe "$dir\plugins\zabbix-agent2-plugin-proctop.exe"
Set-Content "$dir\zabbix_agent2.d\plugins.d\proctop.conf" "Plugins.ProcTop.System.Path=$dir\plugins\zabbix-agent2-plugin-proctop.exe"
Restart-Service "Zabbix Agent 2"
& "$dir\zabbix_agent2.exe" -c "$dir\zabbix_agent2.conf" -t "proctop.top[10,cpu]"
```

The agent configuration must include the `plugins.d` directory
(`Include=.../zabbix_agent2.d/plugins.d/*.conf`, present in the default
configuration).

## Configuration

Optional parameters, in the same `proctop.conf` file:

| Parameter | Default | Effect |
|---|---|---|
| `Plugins.ProcTop.ListenExcludeWindowsRPC` | `true` | Hide the dynamic RPC ports (49152–65535) that `svchost.exe`, `lsass.exe`, `wininit.exe`, `services.exe` and `spoolsv.exe` open on their own. |
| `Plugins.ProcTop.ListenExcludePorts` | *(empty)* | Comma-separated ports to hide from `proctop.net.listen`, e.g. `5353,1900`. |

## Template example

Show the top processes in the operational data of an existing CPU trigger. The
`or 1=1` keeps the trigger working on hosts without the plugin (the item is then
unsupported):

- Item: `proctop.top[10,cpu]`, type *Zabbix agent (active)*, type of information
  *Text*, update interval `2m`, history `1d`.
- Trigger expression:
  ```
  min(/Template/system.cpu.util,5m)>{$CPU.UTIL.CRIT}
  and (length(last(/Template/proctop.top[10,cpu]))>=0 or 1=1)
  ```
- Operational data:
  ```
  Current utilization: {ITEM.LASTVALUE1}
  {ITEM.LASTVALUE2}
  ```

The Zabbix web interface truncates text values to 20 characters in operational
data; notifications (e-mail, webhooks, scripts) receive the full value.

## Compatibility

A loadable plugin talks to the agent through the plugin SDK of the agent's
release branch. Each release therefore ships one build per agent series
(`-zbx7.4-`, `-zbx8.0-`): **use the build matching your agent**, and switch
builds when you upgrade the agent to a new major series.

| Platform | Architectures |
|---|---|
| Windows | amd64 |
| Linux | amd64, arm64, armv7 |

## Building

```sh
go test ./...
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" .
```

To build for another agent series, pin the SDK to the head of its release branch
(see `.github/workflows/release.yml`):

```sh
go mod edit -require=golang.zabbix.com/sdk@<pseudo-version of release/X.Y>
go mod tidy
```

## License

MIT — see [LICENSE](LICENSE) and [NOTICE](NOTICE). This project is not
affiliated with Zabbix SIA.
