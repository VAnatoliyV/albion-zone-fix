package trace

import (
	"net/netip"
	"testing"
	"time"
)

// ICMP «время истекло» от узла 10.1.1.1: внутри — начало нашего UDP-пакета
// (192.168.1.5:40000 → 193.169.238.242:5056).
func timeExceeded(from [4]byte, srcPort uint16) []byte {
	inner := make([]byte, 28)
	inner[0], inner[9] = 0x45, 17
	copy(inner[12:16], []byte{192, 168, 1, 5})
	copy(inner[16:20], []byte{193, 169, 238, 242})
	inner[20], inner[21] = byte(srcPort>>8), byte(srcPort)
	inner[22], inner[23] = 0x13, 0xC0
	icmp := append([]byte{11, 0, 0, 0, 0, 0, 0, 0}, inner...)
	ip := make([]byte, 20)
	ip[0], ip[9] = 0x45, 1
	copy(ip[12:16], from[:])
	copy(ip[16:20], []byte{192, 168, 1, 5})
	return append(ip, icmp...)
}

func TestParseTimeExceeded(t *testing.T) {
	m, ok := ParseICMP(timeExceeded([4]byte{10, 1, 1, 1}, 40000))
	if !ok || m.Type != 11 || m.From != netip.MustParseAddr("10.1.1.1") ||
		m.OrigDst != netip.MustParseAddrPort("193.169.238.242:5056") || m.OrigSrcPort != 40000 {
		t.Fatalf("разбор: %+v %v", m, ok)
	}
	if _, ok := ParseICMP([]byte{0x45, 0, 0}); ok {
		t.Fatal("обрывок принят")
	}
}

// Путь из трёх узлов, сервер не отвечает: трасса должна показать узлы и «тишину» после них.
type fakeNet struct {
	hops  []string
	reply bool
	port  uint16
	dst   netip.AddrPort
	icmp  chan Msg
	udp   chan time.Duration
}

func (f *fakeNet) Send(ttl int) (uint16, error) {
	go func() {
		if ttl <= len(f.hops) {
			f.icmp <- Msg{Type: 11, From: netip.MustParseAddr(f.hops[ttl-1]),
				OrigDst: f.dst, OrigSrcPort: f.port}
		} else if f.reply {
			f.udp <- time.Millisecond
		}
	}()
	return f.port, nil
}

func TestTraceStopsAtSilenceAndReportsHops(t *testing.T) {
	f := &fakeNet{hops: []string{"192.168.1.1", "95.167.1.1", "80.249.208.1"}, port: 40000, dst: netip.MustParseAddrPort("193.169.238.242:5056"), icmp: make(chan Msg, 4), udp: make(chan time.Duration, 4)}
	hops := Run(f, netip.MustParseAddrPort("193.169.238.242:5056"), f.icmp, f.udp, Options{MaxTTL: 8, Wait: 50 * time.Millisecond, StopAfterSilent: 3})
	if len(hops) != 6 || hops[0].IP != "192.168.1.1" || hops[2].IP != "80.249.208.1" || hops[3].IP != "" || hops[5].IP != "" {
		t.Fatalf("трасса: %+v", hops)
	}
	if Reached(hops) {
		t.Fatal("сервер не отвечал, а трасса считает, что дошли")
	}
}

func TestTraceReachesServer(t *testing.T) {
	f := &fakeNet{hops: []string{"192.168.1.1", "95.167.1.1"}, reply: true, port: 40001, dst: netip.MustParseAddrPort("193.169.238.124:5056"), icmp: make(chan Msg, 4), udp: make(chan time.Duration, 4)}
	hops := Run(f, netip.MustParseAddrPort("193.169.238.124:5056"), f.icmp, f.udp, Options{MaxTTL: 8, Wait: 50 * time.Millisecond, StopAfterSilent: 3})
	if !Reached(hops) || len(hops) != 3 || !hops[2].Server {
		t.Fatalf("трасса: %+v", hops)
	}
}
