package main

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"golang.zabbix.com/sdk/conf"
	"golang.zabbix.com/sdk/plugin"
)

const (
	keyTop    = "proctop.top"
	keyConns  = "proctop.net.conns"
	keyListen = "proctop.net.listen"

	defaultTopN = 10
	maxTopN     = 25
)

var (
	_ plugin.Exporter     = (*procTop)(nil)
	_ plugin.Configurator = (*procTop)(nil)
)

// options are read from Plugins.ProcTop.* in the agent configuration.
type options struct {
	// ListenExcludeWindowsRPC hides the dynamic RPC ports (49152-65535) that
	// Windows system services open and close on their own.
	ListenExcludeWindowsRPC bool `conf:"optional,default=true"`
	// ListenExcludePorts is a comma-separated list of ports to hide from
	// proctop.net.listen, e.g. "5353,1900".
	ListenExcludePorts string `conf:"optional"`
}

type procTop struct {
	plugin.Base

	mu       sync.RWMutex
	filter   listenFilter
	sampler  *sampler
	netState netReader
}

func newPlugin() *procTop {
	return &procTop{
		filter:   defaultListenFilter(),
		sampler:  newSampler(takeSnapshot),
		netState: readSockets,
	}
}

// Configure applies the plugin options; invalid options keep the defaults.
func (p *procTop) Configure(_ *plugin.GlobalOptions, private any) {
	f, err := parseOptions(private)
	if err != nil {
		p.Warningf("invalid configuration, using defaults: %s", err)

		f = defaultListenFilter()
	}

	p.mu.Lock()
	p.filter = f
	p.mu.Unlock()
}

// Validate checks the plugin options when the agent starts.
func (p *procTop) Validate(private any) error {
	_, err := parseOptions(private)

	return err
}

func parseOptions(private any) (listenFilter, error) {
	o := options{ListenExcludeWindowsRPC: true}

	if private != nil {
		if err := conf.Unmarshal(private, &o); err != nil {
			return listenFilter{}, fmt.Errorf("cannot read plugin options: %w", err)
		}
	}

	ports, err := parsePortList(o.ListenExcludePorts)
	if err != nil {
		return listenFilter{}, err
	}

	return listenFilter{excludeWindowsRPC: o.ListenExcludeWindowsRPC, excludePorts: ports}, nil
}

func parsePortList(s string) (map[uint16]bool, error) {
	ports := map[uint16]bool{}

	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		n, err := strconv.ParseUint(part, 10, 16)
		if err != nil || n == 0 {
			return nil, fmt.Errorf("invalid port %q in ListenExcludePorts", part)
		}

		ports[uint16(n)] = true
	}

	return ports, nil
}

// Export returns the value of an item key.
func (p *procTop) Export(key string, params []string, _ plugin.ContextProvider) (any, error) {
	switch key {
	case keyTop:
		return p.exportTop(params)
	case keyConns:
		return p.exportConns(params)
	case keyListen:
		return p.exportListen(params)
	default:
		return nil, fmt.Errorf("unknown item key %q", key)
	}
}

// proctop.top[<n>,<cpu|mem>,<text|json>]
func (p *procTop) exportTop(params []string) (any, error) {
	if len(params) > 3 {
		return nil, fmt.Errorf("too many parameters: expected at most 3")
	}

	n, err := parseTopN(param(params, 0))
	if err != nil {
		return nil, err
	}

	format, err := parseFormat(param(params, 2))
	if err != nil {
		return nil, err
	}

	switch mode := param(params, 1); mode {
	case "", "cpu":
		prev, last, err := p.sampler.pair()
		if err != nil {
			return nil, err
		}

		return formatRanking(rankCPU(prev, last, n), format), nil
	case "mem":
		last, err := p.sampler.fresh()
		if err != nil {
			return nil, err
		}

		return formatRanking(rankMem(last, n), format), nil
	default:
		return nil, fmt.Errorf("invalid mode %q: expected cpu or mem", mode)
	}
}

// proctop.net.conns[<n>,<text|json>]
func (p *procTop) exportConns(params []string) (any, error) {
	if len(params) > 2 {
		return nil, fmt.Errorf("too many parameters: expected at most 2")
	}

	n, err := parseTopN(param(params, 0))
	if err != nil {
		return nil, err
	}

	format, err := parseFormat(param(params, 1))
	if err != nil {
		return nil, err
	}

	socks, names, err := p.sockets()
	if err != nil {
		return nil, err
	}

	return formatConns(rankConns(socks, names, n), format), nil
}

// proctop.net.listen[<text|json>]
func (p *procTop) exportListen(params []string) (any, error) {
	if len(params) > 1 {
		return nil, fmt.Errorf("too many parameters: expected at most 1")
	}

	format, err := parseFormat(param(params, 0))
	if err != nil {
		return nil, err
	}

	socks, names, err := p.sockets()
	if err != nil {
		return nil, err
	}

	p.mu.RLock()
	f := p.filter
	p.mu.RUnlock()

	return formatListen(exposedPorts(socks, names, f), format), nil
}

func (p *procTop) sockets() ([]socket, map[int]string, error) {
	snap, err := p.sampler.fresh()
	if err != nil {
		return nil, nil, err
	}

	socks, err := p.netState()
	if err != nil {
		return nil, nil, err
	}

	return socks, snap.names(), nil
}

func param(params []string, i int) string {
	if i < len(params) {
		return strings.TrimSpace(params[i])
	}

	return ""
}

func parseTopN(s string) (int, error) {
	if s == "" {
		return defaultTopN, nil
	}

	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > maxTopN {
		return 0, fmt.Errorf("invalid number of entries %q: expected 1 to %d", s, maxTopN)
	}

	return n, nil
}

type outputFormat int

const (
	formatText outputFormat = iota
	formatJSON
)

func parseFormat(s string) (outputFormat, error) {
	switch s {
	case "", "text":
		return formatText, nil
	case "json":
		return formatJSON, nil
	default:
		return formatText, fmt.Errorf("invalid format %q: expected text or json", s)
	}
}
