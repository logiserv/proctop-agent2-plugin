//go:build windows

package main

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows clients use dynamic ports from 49152 (netsh int ipv4 show dynamicport).
const ephemeralPortStart = 49152

var (
	iphlpapi                = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetExtendedTcpTable = iphlpapi.NewProc("GetExtendedTcpTable")
	procGetExtendedUdpTable = iphlpapi.NewProc("GetExtendedUdpTable")
)

const (
	afInet  = 2
	afInet6 = 23

	tcpTableOwnerPIDAll = 5 // TCP_TABLE_OWNER_PID_ALL
	udpTableOwnerPID    = 1 // UDP_TABLE_OWNER_PID

	mibTCPStateListen      = 2
	mibTCPStateEstablished = 5

	errorInsufficientBuffer = 122

	// Row sizes of MIB_TCPROW_OWNER_PID, MIB_TCP6ROW_OWNER_PID,
	// MIB_UDPROW_OWNER_PID and MIB_UDP6ROW_OWNER_PID.
	tcp4RowSize = 24
	tcp6RowSize = 56
	udp4RowSize = 12
	udp6RowSize = 28
)

// readSockets lists TCP and UDP sockets (IPv4 and IPv6) with their owning PID.
func readSockets() ([]socket, error) {
	var socks []socket

	for _, t := range []struct {
		proc  *windows.LazyProc
		af    uint32
		class uint32
		parse func([]byte) []socket
	}{
		{procGetExtendedTcpTable, afInet, tcpTableOwnerPIDAll, parseTCP4},
		{procGetExtendedTcpTable, afInet6, tcpTableOwnerPIDAll, parseTCP6},
		{procGetExtendedUdpTable, afInet, udpTableOwnerPID, parseUDP4},
		{procGetExtendedUdpTable, afInet6, udpTableOwnerPID, parseUDP6},
	} {
		buf, err := extendedTable(t.proc, t.af, t.class)
		if err != nil {
			return nil, err
		}

		socks = append(socks, t.parse(buf)...)
	}

	return socks, nil
}

func extendedTable(proc *windows.LazyProc, af, class uint32) ([]byte, error) {
	size := uint32(64 * 1024)

	for range 8 {
		buf := make([]byte, size)

		r, _, _ := proc.Call(
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(unsafe.Pointer(&size)),
			0, // unsorted
			uintptr(af),
			uintptr(class),
			0,
		)

		switch r {
		case 0:
			return buf[:size], nil
		case errorInsufficientBuffer:
			size += 16 * 1024 // connections may open between two calls

			continue
		default:
			return nil, fmt.Errorf("%s failed: %w", proc.Name, windows.Errno(r))
		}
	}

	return nil, fmt.Errorf("%s: socket table keeps growing", proc.Name)
}

// rows returns the rows of a MIB_*TABLE_OWNER_PID: a uint32 count followed by
// fixed-size rows (the first row starts at offset 4).
func rows(buf []byte, rowSize int) [][]byte {
	if len(buf) < 4 {
		return nil
	}

	n := int(binary.LittleEndian.Uint32(buf[:4]))
	out := make([][]byte, 0, n)

	for i := range n {
		start := 4 + i*rowSize
		if start+rowSize > len(buf) {
			break
		}

		out = append(out, buf[start:start+rowSize])
	}

	return out
}

// Ports are stored in network byte order in the low 16 bits of a DWORD.
func rowPort(b []byte) uint16 { return binary.BigEndian.Uint16(b[:2]) }

func rowPID(b []byte) int { return int(binary.LittleEndian.Uint32(b[:4])) }

func addr4(b []byte) netip.Addr { return netip.AddrFrom4([4]byte(b[:4])) }

func addr6(b []byte) netip.Addr { return netip.AddrFrom16([16]byte(b[:16])) }

func tcpState(dw uint32) sockState {
	switch dw {
	case mibTCPStateListen:
		return stateListen
	case mibTCPStateEstablished:
		return stateEstablished
	default:
		return stateOther
	}
}

// MIB_TCPROW_OWNER_PID: state, local addr, local port, remote addr, remote port, pid.
func parseTCP4(buf []byte) []socket {
	var out []socket

	for _, r := range rows(buf, tcp4RowSize) {
		out = append(out, socket{
			proto:  "tcp",
			state:  tcpState(binary.LittleEndian.Uint32(r[0:4])),
			local:  netip.AddrPortFrom(addr4(r[4:8]), rowPort(r[8:12])),
			remote: netip.AddrPortFrom(addr4(r[12:16]), rowPort(r[16:20])),
			pid:    rowPID(r[20:24]),
		})
	}

	return out
}

// MIB_TCP6ROW_OWNER_PID: local addr[16], scope, local port, remote addr[16],
// scope, remote port, state, pid.
func parseTCP6(buf []byte) []socket {
	var out []socket

	for _, r := range rows(buf, tcp6RowSize) {
		out = append(out, socket{
			proto:  "tcp",
			local:  netip.AddrPortFrom(addr6(r[0:16]), rowPort(r[20:24])),
			remote: netip.AddrPortFrom(addr6(r[24:40]), rowPort(r[44:48])),
			state:  tcpState(binary.LittleEndian.Uint32(r[48:52])),
			pid:    rowPID(r[52:56]),
		})
	}

	return out
}

// MIB_UDPROW_OWNER_PID: local addr, local port, pid.
func parseUDP4(buf []byte) []socket {
	var out []socket

	for _, r := range rows(buf, udp4RowSize) {
		out = append(out, socket{
			proto: "udp",
			local: netip.AddrPortFrom(addr4(r[0:4]), rowPort(r[4:8])),
			pid:   rowPID(r[8:12]),
		})
	}

	return out
}

// MIB_UDP6ROW_OWNER_PID: local addr[16], scope, local port, pid.
func parseUDP6(buf []byte) []socket {
	var out []socket

	for _, r := range rows(buf, udp6RowSize) {
		out = append(out, socket{
			proto: "udp",
			local: netip.AddrPortFrom(addr6(r[0:16]), rowPort(r[20:24])),
			pid:   rowPID(r[24:28]),
		})
	}

	return out
}
