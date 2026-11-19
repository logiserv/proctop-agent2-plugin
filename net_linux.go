//go:build linux

package main

import (
	"os"
	"strconv"
)

// Default start of net.ipv4.ip_local_port_range: ports above belong to clients.
const ephemeralPortStart = 32768

// readSockets lists TCP and UDP sockets from /proc/net and finds their owner
// through /proc/[pid]/fd. File descriptors of processes owned by another user
// are not readable when the agent runs as "zabbix": those sockets keep pid -1
// and are reported as "(unknown process)", with correct ports and counts.
func readSockets() ([]socket, error) {
	var all []procSocket

	for _, src := range []struct{ file, proto string }{
		{"/proc/net/tcp", "tcp"},
		{"/proc/net/tcp6", "tcp"},
		{"/proc/net/udp", "udp"},
		{"/proc/net/udp6", "udp"},
	} {
		f, err := os.Open(src.file)
		if err != nil {
			// tcp6/udp6 are missing when IPv6 is disabled.
			continue
		}

		socks, err := parseProcNet(f, src.proto)
		f.Close()

		if err != nil {
			return nil, err
		}

		all = append(all, socks...)
	}

	owners := socketOwners()
	out := make([]socket, 0, len(all))

	for _, s := range all {
		if pid, ok := owners[s.inode]; ok {
			s.pid = pid
		}

		out = append(out, s.socket)
	}

	return out, nil
}

// socketOwners maps socket inodes to the PID holding them.
func socketOwners() map[uint64]int {
	owners := map[uint64]int{}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return owners
	}

	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}

		fdDir := "/proc/" + e.Name() + "/fd"

		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue // another user's process, or already exited
		}

		for _, fd := range fds {
			target, err := os.Readlink(fdDir + "/" + fd.Name())
			if err != nil {
				continue
			}

			if inode, ok := parseSocketLink(target); ok {
				owners[inode] = pid
			}
		}
	}

	return owners
}
