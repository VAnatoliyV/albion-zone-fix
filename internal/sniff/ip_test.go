package sniff

import (
	"bytes"
	"testing"
)

func ipv4UDP(src, dst [4]byte, sport, dport uint16, payload []byte) []byte {
	b := make([]byte, 20+8+len(payload))
	b[0] = 0x45
	b[9] = 17
	copy(b[12:16], src[:])
	copy(b[16:20], dst[:])
	b[20], b[21] = byte(sport>>8), byte(sport)
	b[22], b[23] = byte(dport>>8), byte(dport)
	copy(b[28:], payload)
	return b
}

func TestParseIPv4(t *testing.T) {
	me, srv := [4]byte{192, 168, 1, 5}, [4]byte{5, 188, 125, 22}
	in := ipv4UDP(srv, me, 5056, 51000, []byte{9, 8, 7})
	addr, pl, ok := ParseIP(in, false)
	if !ok || addr != "5.188.125.22:5056" || !bytes.Equal(pl, []byte{9, 8, 7}) {
		t.Fatalf("входящий: %v %q %v", ok, addr, pl)
	}
	out := ipv4UDP(me, srv, 51000, 5056, []byte{1})
	addr, _, ok = ParseIP(out, true)
	if !ok || addr != "5.188.125.22:5056" {
		t.Fatalf("исходящий: %v %q", ok, addr)
	}
}

func TestParseRejectsTCPAndShort(t *testing.T) {
	p := ipv4UDP([4]byte{1, 1, 1, 1}, [4]byte{2, 2, 2, 2}, 1, 2, nil)
	p[9] = 6
	if _, _, ok := ParseIP(p, false); ok {
		t.Fatal("TCP принят")
	}
	if _, _, ok := ParseIP([]byte{0x45, 0}, false); ok {
		t.Fatal("обрывок принят")
	}
}

func TestParseIPv6(t *testing.T) {
	b := make([]byte, 40+8+2)
	b[0] = 0x60
	b[6] = 17
	b[8], b[23] = 0x2a, 1     // src 2a00::1
	b[40], b[41] = 0x13, 0xC0 // порт 5056
	b[48], b[49] = 4, 2
	addr, pl, ok := ParseIP(b, false)
	if !ok || addr != "[2a00::1]:5056" || len(pl) != 2 {
		t.Fatalf("ipv6: %v %q %v", ok, addr, pl)
	}
}

func TestForCollector(t *testing.T) {
	me, srv := [4]byte{192, 168, 1, 5}, [4]byte{193, 169, 238, 17}
	if !ForCollector(ipv4UDP(srv, me, 5056, 51000, []byte{1})) || !ForCollector(ipv4UDP(me, srv, 51000, 5056, nil)) {
		t.Fatal("UDP 5056 в обе стороны нужен сборщику")
	}
	if ForCollector(ipv4UDP(srv, me, 5055, 51000, []byte{1})) {
		t.Fatal("5055 сборщик не слушает")
	}
	tcp := ipv4UDP(srv, me, 5056, 51000, nil)
	tcp[9] = 6
	if !ForCollector(tcp) {
		t.Fatal("TCP 5056 сборщик тоже слушает")
	}
	tcp[9] = 1
	if ForCollector(tcp) {
		t.Fatal("ICMP принят")
	}
	v6 := make([]byte, 60)
	v6[0] = 0x60
	if ForCollector(v6) || ForCollector([]byte{0x45, 0, 0}) {
		t.Fatal("IPv6 или обрывок принят")
	}
}
