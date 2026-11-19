package main

// Parsers for the Linux /proc files. They have no build constraint so that
// they are unit-tested on every platform.

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// clockTicks is USER_HZ, the unit of utime and stime in /proc/[pid]/stat.
// It is 100 on every mainstream Linux architecture.
const clockTicks = 100

// parseProcStat reads the command name and cumulative CPU time from the
// content of /proc/[pid]/stat. The name is between the first "(" and the
// last ")" since it may contain spaces or parentheses itself.
func parseProcStat(data string) (name string, cpu time.Duration, err error) {
	open := strings.IndexByte(data, '(')
	closing := strings.LastIndexByte(data, ')')

	if open < 0 || closing < open {
		return "", 0, fmt.Errorf("malformed stat line")
	}

	name = data[open+1 : closing]

	// Fields after the name start at field 3 (state); utime and stime are
	// fields 14 and 15.
	fields := strings.Fields(data[closing+1:])
	if len(fields) < 13 {
		return "", 0, fmt.Errorf("malformed stat line: %d fields", len(fields))
	}

	utime, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("utime: %w", err)
	}

	stime, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("stime: %w", err)
	}

	ticks := utime + stime

	return name, time.Duration(ticks) * time.Second / clockTicks, nil
}

// parseStatmRSS returns the resident set size in pages from /proc/[pid]/statm.
func parseStatmRSS(data string) (uint64, error) {
	fields := strings.Fields(data)
	if len(fields) < 2 {
		return 0, fmt.Errorf("malformed statm line")
	}

	return strconv.ParseUint(fields[1], 10, 64)
}

// countCPUs counts the "cpuN" lines of /proc/stat.
func countCPUs(r io.Reader) int {
	n := 0
	sc := bufio.NewScanner(r)

	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "cpu") && len(line) > 3 && line[3] >= '0' && line[3] <= '9' {
			n++
		}
	}

	return n
}

// Socket states of /proc/net/tcp (include/net/tcp_states.h).
const (
	procTCPEstablished = "01"
	procTCPListen      = "0A"
)

// procSocket is one line of /proc/net/{tcp,tcp6,udp,udp6}, before the owning
// process is known.
type procSocket struct {
	socket
	inode uint64
}

// parseProcNet reads /proc/net/{tcp,tcp6,udp,udp6}.
func parseProcNet(r io.Reader, proto string) ([]procSocket, error) {
	var out []procSocket

	sc := bufio.NewScanner(r)
	sc.Scan() // header line

	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 10 {
			continue
		}

		local, err := parseProcAddr(fields[1])
		if err != nil {
			return nil, err
		}

		remote, err := parseProcAddr(fields[2])
		if err != nil {
			return nil, err
		}

		inode, err := strconv.ParseUint(fields[9], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("inode: %w", err)
		}

		s := procSocket{socket: socket{proto: proto, local: local, remote: remote, pid: -1}, inode: inode}

		if proto == "tcp" {
			switch strings.ToUpper(fields[3]) {
			case procTCPListen:
				s.state = stateListen
			case procTCPEstablished:
				s.state = stateEstablished
			}
		}

		out = append(out, s)
	}

	return out, sc.Err()
}

// parseProcAddr decodes "0100007F:0016" (IPv4) or a 32-hex-digit IPv6
// address. The address is a sequence of 32-bit words in host byte order
// (little-endian on every architecture we build for); the port is plain hex.
func parseProcAddr(s string) (netip.AddrPort, error) {
	hostHex, portHex, ok := strings.Cut(s, ":")
	if !ok {
		return netip.AddrPort{}, fmt.Errorf("malformed address %q", s)
	}

	raw, err := hex.DecodeString(hostHex)
	if err != nil || (len(raw) != 4 && len(raw) != 16) {
		return netip.AddrPort{}, fmt.Errorf("malformed address %q", s)
	}

	p, err := strconv.ParseUint(portHex, 16, 16)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("malformed port %q", s)
	}

	// Each 32-bit word is stored little-endian: reverse its bytes.
	ordered := make([]byte, len(raw))
	for i := 0; i < len(raw); i += 4 {
		binary.BigEndian.PutUint32(ordered[i:], binary.LittleEndian.Uint32(raw[i:]))
	}

	var addr netip.Addr
	if len(ordered) == 4 {
		addr = netip.AddrFrom4([4]byte(ordered))
	} else {
		addr = netip.AddrFrom16([16]byte(ordered))
	}

	return netip.AddrPortFrom(addr, uint16(p)), nil
}

// parseSocketLink returns the inode of a /proc/[pid]/fd link target such as
// "socket:[12345]".
func parseSocketLink(target string) (uint64, bool) {
	rest, ok := strings.CutPrefix(target, "socket:[")
	if !ok {
		return 0, false
	}

	rest, ok = strings.CutSuffix(rest, "]")
	if !ok {
		return 0, false
	}

	inode, err := strconv.ParseUint(rest, 10, 64)

	return inode, err == nil
}
