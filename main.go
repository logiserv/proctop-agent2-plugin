// Command zabbix-agent2-plugin-proctop is a loadable plugin for Zabbix agent 2
// that reports the processes using the most CPU or memory, the processes
// holding the most TCP connections, and the network ports exposed by each
// process.
package main

import (
	"fmt"
	"os"

	"golang.zabbix.com/sdk/errs"
	"golang.zabbix.com/sdk/plugin"
	"golang.zabbix.com/sdk/plugin/container"
)

// version is set at build time with -ldflags "-X main.version=<tag>".
var version = "dev"

const pluginName = "ProcTop"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-V" || os.Args[1] == "--version") {
		fmt.Printf("zabbix-agent2-plugin-proctop %s\n", version)

		return
	}

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	p := newPlugin()

	err := plugin.RegisterMetrics(
		p, pluginName,
		keyTop, "Top N processes by CPU or memory usage.",
		keyConns, "Top N processes by number of established TCP connections.",
		keyListen, "Network ports exposed by each process (loopback excluded).",
	)
	if err != nil {
		return errs.Wrap(err, "failed to register metrics")
	}

	h, err := container.NewHandler(pluginName)
	if err != nil {
		return errs.Wrap(err, "failed to create plugin handler")
	}

	p.Logger = h

	if err := h.Execute(); err != nil {
		return errs.Wrap(err, "failed to execute plugin handler")
	}

	return nil
}
