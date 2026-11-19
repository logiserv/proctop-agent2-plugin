package main

import (
	"net/netip"
	"strings"
	"testing"
	"time"
)

// Sample of /proc/net/tcp: sshd listening on 0.0.0.0:22, a service on
// 127.0.0.1:631 (loopback), and an established connection to 93.184.216.34:443.
const procNetTCP = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 1001 1 0000000000000000 100 0 0 10 0
   1: 0100007F:0277 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 1002 1 0000000000000000 100 0 0 10 0
   2: 0F02000A:C350 22D8B85D:01BB 01 00000000:00000000 02:000A7C1E 00000000  1000        0 1003 2 0000000000000000 20 4 30 10 -1
`

// IPv6: [::]:80 listening.
const procNetTCP6 = `  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000000000000000000000000000:0050 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 2001 1 0000000000000000 100 0 0 10 0
   1: 00000000000000000000000001000000:1F90 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 2002 1 0000000000000000 100 0 0 10 0
`

func TestParseProcNetTCP(t *testing.T) {
	socks, err := parseProcNet(strings.NewReader(procNetTCP), "tcp")
	if err != nil {
		t.Fatal(err)
	}

	if len(socks) != 3 {
		t.Fatalf("got %d sockets, want 3", len(socks))
	}

	if socks[0].local != netip.MustParseAddrPort("0.0.0.0:22") || socks[0].state != stateListen || socks[0].inode != 1001 {
		t.Errorf("socket 0 = %+v", socks[0])
	}

	if socks[1].local != netip.MustParseAddrPort("127.0.0.1:631") {
		t.Errorf("socket 1 local = %s", socks[1].local)
	}

	if socks[2].remote != netip.MustParseAddrPort("93.184.216.34:443") || socks[2].state != stateEstablished {
		t.Errorf("socket 2 = %+v", socks[2])
	}

	if socks[2].local != netip.MustParseAddrPort("10.0.2.15:50000") {
		t.Errorf("socket 2 local = %s", socks[2].local)
	}
}

func TestParseProcNetTCP6(t *testing.T) {
	socks, err := parseProcNet(strings.NewReader(procNetTCP6), "tcp")
	if err != nil {
		t.Fatal(err)
	}

	if socks[0].local != netip.MustParseAddrPort("[::]:80") {
		t.Errorf("socket 0 local = %s", socks[0].local)
	}

	if socks[1].local != netip.MustParseAddrPort("[::1]:8080") {
		t.Errorf("socket 1 local = %s, want [::1]:8080", socks[1].local)
	}
}

func TestParseProcStat(t *testing.T) {
	// The name contains a space and a parenthesis.
	line := "1234 (my (odd) proc) S 1 1234 1234 0 -1 4194560 100 0 0 0 250 50 0 0 20 0 1 0 100 1000 200 18446744073709551615"

	name, cpu, err := parseProcStat(line)
	if err != nil {
		t.Fatal(err)
	}

	if name != "my (odd) proc" {
		t.Errorf("name = %q", name)
	}

	// utime 250 + stime 50 ticks at 100 Hz = 3 s.
	if cpu != 3*time.Second {
		t.Errorf("cpu = %s, want 3s", cpu)
	}
}

func TestParseSocketLink(t *testing.T) {
	if inode, ok := parseSocketLink("socket:[12345]"); !ok || inode != 12345 {
		t.Errorf("got %d, %v", inode, ok)
	}

	if _, ok := parseSocketLink("/dev/null"); ok {
		t.Error("/dev/null is not a socket")
	}
}

func TestExposedPorts(t *testing.T) {
	names := map[int]string{10: "sshd", 20: "svchost.exe", 30: "nginx", 40: "dnsmasq"}
	socks := []socket{
		{proto: "tcp", local: netip.MustParseAddrPort("0.0.0.0:22"), state: stateListen, pid: 10},
		{proto: "tcp", local: netip.MustParseAddrPort("[::]:22"), state: stateListen, pid: 10},
		{proto: "tcp", local: netip.MustParseAddrPort("127.0.0.1:631"), state: stateListen, pid: 30},          // loopback
		{proto: "tcp", local: netip.MustParseAddrPort("[::ffff:127.0.0.1]:632"), state: stateListen, pid: 30}, // mapped loopback
		{proto: "tcp", local: netip.MustParseAddrPort("0.0.0.0:49670"), state: stateListen, pid: 20},          // Windows RPC
		{proto: "tcp", local: netip.MustParseAddrPort("0.0.0.0:443"), state: stateEstablished, pid: 30},       // not listening
		{proto: "tcp", local: netip.MustParseAddrPort("0.0.0.0:80"), state: stateListen, pid: 99},             // unknown owner
		{proto: "udp", local: netip.MustParseAddrPort("0.0.0.0:53"), pid: 40},
		{proto: "udp", local: netip.MustParseAddrPort("0.0.0.0:60000"), pid: 40}, // ephemeral client port
		{proto: "udp", local: netip.MustParseAddrPort("0.0.0.0:5353"), pid: 40},  // excluded by option
	}

	f := defaultListenFilter()
	f.excludePorts[5353] = true

	got := formatListen(exposedPorts(socks, names, f), formatText)
	want := strings.Join([]string{
		"tcp/22 sshd [0.0.0.0, ::]",
		"tcp/80 (unknown process) [0.0.0.0]",
		"udp/53 dnsmasq [0.0.0.0]",
	}, "\n")

	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}

	f.excludeWindowsRPC = false
	if !strings.Contains(formatListen(exposedPorts(socks, names, f), formatText), "tcp/49670 svchost.exe") {
		t.Error("RPC ports should be listed when ListenExcludeWindowsRPC is false")
	}

	if got := formatListen(nil, formatJSON); got != "[]" {
		t.Errorf("empty JSON = %q, want []", got)
	}
}

func TestRankConns(t *testing.T) {
	names := map[int]string{1: "chrome.exe", 2: "chrome.exe", 3: "sqlservr.exe"}
	est := func(pid int, remote string) socket {
		return socket{
			proto: "tcp", state: stateEstablished, pid: pid,
			local:  netip.MustParseAddrPort("10.0.0.5:50000"),
			remote: netip.MustParseAddrPort(remote),
		}
	}

	socks := []socket{
		est(1, "142.250.1.1:443"),
		est(1, "142.250.1.1:443"),
		est(2, "142.250.1.2:443"),
		est(3, "10.0.0.9:1433"),
		est(3, "127.0.0.1:1433"), // loopback: ignored
		{proto: "tcp", state: stateListen, pid: 3, local: netip.MustParseAddrPort("0.0.0.0:1433")},
	}

	got := rankConns(socks, names, 10)

	if len(got) != 2 || got[0].Process != "chrome.exe" || got[0].Connections != 3 || got[0].Remotes != 2 {
		t.Fatalf("unexpected ranking: %+v", got)
	}

	if got[0].TopRemotes[0].Address != "142.250.1.1" || got[0].TopRemotes[0].Connections != 2 {
		t.Errorf("top remote = %+v", got[0].TopRemotes[0])
	}

	if got[1].Process != "sqlservr.exe" || got[1].Connections != 1 {
		t.Errorf("sqlservr.exe = %+v", got[1])
	}
}

func TestParseOptions(t *testing.T) {
	f, err := parseOptions(nil)
	if err != nil || !f.excludeWindowsRPC {
		t.Fatalf("defaults: %+v, %v", f, err)
	}

	if _, err := parsePortList("53, 5353"); err != nil {
		t.Error(err)
	}

	if _, err := parsePortList("53,abc"); err == nil {
		t.Error("invalid port should be rejected")
	}
}

func TestParams(t *testing.T) {
	if n, err := parseTopN(""); err != nil || n != defaultTopN {
		t.Errorf("default N = %d, %v", n, err)
	}

	if _, err := parseTopN("26"); err == nil {
		t.Error("N above the maximum should be rejected")
	}

	if _, err := parseFormat("xml"); err == nil {
		t.Error("unknown format should be rejected")
	}
}
