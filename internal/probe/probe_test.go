package probe

import (
	"net"
	"testing"
	"time"
)

// Локальный «игровой сервер»: отвечает только на CONNECT (команда 2).
func fakeServer(t *testing.T, answer bool) string {
	c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	go func() {
		buf := make([]byte, 2000)
		for {
			n, from, err := c.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if answer && n >= 13 && buf[12] == 2 {
				c.WriteToUDP([]byte{0, 1, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 1}, from)
			}
		}
	}()
	return c.LocalAddr().String()
}

func TestProbeSeesWhoAnswers(t *testing.T) {
	alive, dead := fakeServer(t, true), fakeServer(t, false)
	res := Run([]Target{{Name: "alive", Addr: alive}, {Name: "dead", Addr: dead}}, 300*time.Millisecond)
	if len(res) != 2 || !res[0].OK || res[0].Ms < 0 || res[1].OK {
		t.Fatalf("результат: %+v", res)
	}
}

func TestConnectPacketLooksLikeTheGame(t *testing.T) {
	p := connectPacket()
	if len(p) != 56 || p[0] != 0xff || p[1] != 0xff || p[3] != 1 || p[12] != 2 {
		t.Fatalf("пакет: %x", p)
	}
	if q := connectPacket(); string(q[8:12]) == string(p[8:12]) {
		t.Fatal("код соединения не случайный")
	}
}
