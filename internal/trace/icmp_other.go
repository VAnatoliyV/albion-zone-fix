//go:build !windows

package trace

import (
	"net"
	"net/netip"
)

// OpenICMP слушает ICMP системным сокетом (нужен root). Только для проверки
// трассировки вне Windows; в программе под Windows ICMP ловит WinDivert.
func OpenICMP(string) (<-chan Msg, func(), error) {
	c, err := net.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return nil, nil, err
	}
	out := make(chan Msg, 64)
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := c.ReadFrom(buf)
			if err != nil {
				return
			}
			a, _ := netip.ParseAddr(from.String())
			if m, ok := ParseICMPBody(a, buf[:n]); ok {
				select {
				case out <- m:
				default:
				}
			}
		}
	}()
	return out, func() { c.Close() }, nil
}
