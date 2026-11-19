package main

import (
	"net/netip"
	"sort"
	"strings"
)

type sockState int

const (
	stateOther sockState = iota
	stateListen
	stateEstablished
)

// socket is one TCP or UDP socket of the machine.
type socket struct {
	proto  string // "tcp" or "udp"
	local  netip.AddrPort
	remote netip.AddrPort
	state  sockState // TCP state; UDP sockets are stateOther
	pid    int       // owning process, -1 when it cannot be determined
}

type netReader func() ([]socket, error)

// unknownProcess names sockets whose owner cannot be read, e.g. processes of
// another user on Linux when the agent does not run as root.
const unknownProcess = "(unknown process)"

func processName(names map[int]string, pid int) string {
	if name, ok := names[pid]; ok && name != "" {
		return name
	}

	return unknownProcess
}

// windowsRPCProcesses open dynamic RPC endpoints (49152-65535) on their own.
var windowsRPCProcesses = map[string]bool{
	"svchost.exe":  true,
	"lsass.exe":    true,
	"wininit.exe":  true,
	"services.exe": true,
	"spoolsv.exe":  true,
}

const windowsDynamicPortStart = 49152

type listenFilter struct {
	excludeWindowsRPC bool
	excludePorts      map[uint16]bool
}

func defaultListenFilter() listenFilter {
	return listenFilter{excludeWindowsRPC: true, excludePorts: map[uint16]bool{}}
}

// listenEntry is one exposed port of one process.
type listenEntry struct {
	Proto     string   `json:"proto"`
	Port      uint16   `json:"port"`
	Process   string   `json:"process"`
	Addresses []string `json:"addresses"`
}

// exposedPorts lists the ports reachable from other machines: TCP sockets in
// LISTEN state and bound UDP sockets, excluding loopback addresses, the
// ephemeral UDP ports used by clients and the configured exclusions.
func exposedPorts(socks []socket, names map[int]string, f listenFilter) []listenEntry {
	type key struct {
		proto   string
		port    uint16
		process string
	}

	groups := map[key]map[string]bool{}

	for _, s := range socks {
		switch {
		case s.proto == "tcp" && s.state != stateListen:
			continue
		case s.proto == "udp" && (s.remote.Port() != 0 || s.local.Port() >= ephemeralPortStart):
			// Connected UDP sockets and ephemeral ports belong to clients.
			continue
		}

		addr := s.local.Addr().Unmap()
		if addr.IsLoopback() || s.local.Port() == 0 {
			continue
		}

		port := s.local.Port()
		if f.excludePorts[port] {
			continue
		}

		name := processName(names, s.pid)
		if f.excludeWindowsRPC && s.proto == "tcp" && port >= windowsDynamicPortStart &&
			windowsRPCProcesses[strings.ToLower(name)] {
			continue
		}

		k := key{proto: s.proto, port: port, process: name}
		if groups[k] == nil {
			groups[k] = map[string]bool{}
		}

		groups[k][addr.String()] = true
	}

	entries := make([]listenEntry, 0, len(groups))

	for k, addrs := range groups {
		list := make([]string, 0, len(addrs))
		for a := range addrs {
			list = append(list, a)
		}

		sort.Strings(list)
		entries = append(entries, listenEntry{Proto: k.proto, Port: k.port, Process: k.process, Addresses: list})
	}

	// Stable order so that an unchanged state gives an identical value.
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Proto != b.Proto {
			return a.Proto < b.Proto
		}

		if a.Port != b.Port {
			return a.Port < b.Port
		}

		return a.Process < b.Process
	})

	return entries
}

// connEntry is the number of established TCP connections of a process name.
type connEntry struct {
	Process     string        `json:"process"`
	Connections int           `json:"connections"`
	Remotes     int           `json:"remotes"`
	TopRemotes  []remoteCount `json:"top_remotes"`
	byRemote    map[string]int
}

type remoteCount struct {
	Address     string `json:"address"`
	Connections int    `json:"connections"`
}

const topRemotes = 3

// rankConns ranks process names by established TCP connections to other
// machines (loopback connections are ignored).
func rankConns(socks []socket, names map[int]string, n int) []connEntry {
	groups := map[string]*connEntry{}

	for _, s := range socks {
		if s.proto != "tcp" || s.state != stateEstablished {
			continue
		}

		remote := s.remote.Addr().Unmap()
		if remote.IsLoopback() || !remote.IsValid() {
			continue
		}

		name := processName(names, s.pid)
		key := strings.ToLower(name)

		e, ok := groups[key]
		if !ok {
			e = &connEntry{Process: name, byRemote: map[string]int{}}
			groups[key] = e
		}

		e.Connections++
		e.byRemote[remote.String()]++
	}

	entries := make([]connEntry, 0, len(groups))

	for _, e := range groups {
		e.Remotes = len(e.byRemote)

		for addr, c := range e.byRemote {
			e.TopRemotes = append(e.TopRemotes, remoteCount{Address: addr, Connections: c})
		}

		sort.Slice(e.TopRemotes, func(i, j int) bool {
			if e.TopRemotes[i].Connections != e.TopRemotes[j].Connections {
				return e.TopRemotes[i].Connections > e.TopRemotes[j].Connections
			}

			return e.TopRemotes[i].Address < e.TopRemotes[j].Address
		})

		if len(e.TopRemotes) > topRemotes {
			e.TopRemotes = e.TopRemotes[:topRemotes]
		}

		entries = append(entries, *e)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Connections != entries[j].Connections {
			return entries[i].Connections > entries[j].Connections
		}

		return strings.ToLower(entries[i].Process) < strings.ToLower(entries[j].Process)
	})

	if len(entries) > n {
		entries = entries[:n]
	}

	return entries
}
