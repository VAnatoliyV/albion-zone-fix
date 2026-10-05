package trace

import (
	"net"
	"net/netip"
	"time"

	"golang.org/x/net/ipv4"

	"albionzonefix/internal/probe"
)

// UDPSender шлёт пакет подключения с одного и того же порта, меняя TTL.
// Ответы самого сервера (если дошли) приходят в канал UDP.
type UDPSender struct {
	c   *net.UDPConn
	p   *ipv4.Conn
	UDP chan time.Duration
}

func NewUDPSender(dst netip.AddrPort) (*UDPSender, error) {
	c, err := net.DialUDP("udp4", nil, net.UDPAddrFromAddrPort(dst))
	if err != nil {
		return nil, err
	}
	s := &UDPSender{c: c, p: ipv4.NewConn(c), UDP: make(chan time.Duration, 8)}
	go func() {
		buf := make([]byte, 2048)
		for {
			if _, err := c.Read(buf); err != nil {
				if ne, ok := err.(net.Error); ok && !ne.Timeout() {
					return
				}
				continue
			}
			select {
			case s.UDP <- 0:
			default:
			}
		}
	}()
	return s, nil
}

func (s *UDPSender) Send(ttl int) (uint16, error) {
	if err := s.p.SetTTL(ttl); err != nil {
		return 0, err
	}
	_, err := s.c.Write(probe.ConnectPacket())
	return uint16(s.c.LocalAddr().(*net.UDPAddr).Port), err
}

func (s *UDPSender) Close() { s.c.Close() }
