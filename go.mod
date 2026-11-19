module github.com/logiserv/proctop-agent2-plugin

go 1.25.12

require (
	golang.org/x/sys v0.47.0
	// Branch release/7.4 of https://git.zabbix.com/scm/ap/plugin-support.git
	// (commit 4735c97f7e8b). The release workflow swaps it for release/8.0
	// to build the 8.0 binaries; keep both series in sync with the agent.
	golang.zabbix.com/sdk v1.2.2-0.20261027065228-4735c97f7e8b
)

require (
	github.com/Microsoft/go-winio v0.6.0 // indirect
	golang.org/x/mod v0.24.0 // indirect
	golang.org/x/sync v0.14.0 // indirect
	golang.org/x/tools v0.33.0 // indirect
)
