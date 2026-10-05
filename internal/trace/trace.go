// Пакет trace — трассировка до игрового сервера Albion тем же пакетом подключения,
// что шлёт игра, с растущим TTL. Показывает, на каком узле пакеты пропадают:
// ещё в России (режет провайдер) или уже у Albion (сервер не отвечает).
package trace

import (
	"net/netip"
	"time"
)

// Msg — разобранный ICMP: «время истекло» (11) или «недоступен» (3) с началом нашего пакета внутри.
type Msg struct {
	Type        byte
	From        netip.Addr
	OrigDst     netip.AddrPort
	OrigSrcPort uint16
}

// ParseICMP разбирает сырой IPv4-пакет с ICMP.
func ParseICMP(b []byte) (Msg, bool) {
	if len(b) < 20 || b[0]>>4 != 4 || b[9] != 1 {
		return Msg{}, false
	}
	ihl := int(b[0]&0x0F) * 4
	if len(b) < ihl {
		return Msg{}, false
	}
	from, _ := netip.AddrFromSlice(b[12:16])
	return ParseICMPBody(from, b[ihl:])
}

// ParseICMPBody — то же для ICMP без IP-заголовка (так отдаёт системный сокет).
func ParseICMPBody(from netip.Addr, icmp []byte) (Msg, bool) {
	if len(icmp) < 8+20+8 {
		return Msg{}, false
	}
	t := icmp[0]
	if t != 11 && t != 3 {
		return Msg{}, false
	}
	inner := icmp[8:]
	iihl := int(inner[0]&0x0F) * 4
	if inner[0]>>4 != 4 || inner[9] != 17 || len(inner) < iihl+8 {
		return Msg{}, false
	}
	dst, _ := netip.AddrFromSlice(inner[16:20])
	udp := inner[iihl:]
	sport := uint16(udp[0])<<8 | uint16(udp[1])
	dport := uint16(udp[2])<<8 | uint16(udp[3])
	return Msg{Type: t, From: from, OrigDst: netip.AddrPortFrom(dst, dport), OrigSrcPort: sport}, true
}

// Hop — один шаг трассы. Пустой IP — узел не ответил.
type Hop struct {
	TTL    int    `json:"ttl"`
	IP     string `json:"ip"`
	Name   string `json:"name,omitempty"`
	Ms     int    `json:"ms"`
	Server bool   `json:"server"` // это ответ самого сервера игры
}

// Sender отправляет пакет подключения с заданным TTL и говорит, с какого порта.
type Sender interface {
	Send(ttl int) (srcPort uint16, err error)
}

type Options struct {
	MaxTTL          int
	Wait            time.Duration
	StopAfterSilent int // столько молчащих узлов подряд — дальше не идём
}

func Run(s Sender, dst netip.AddrPort, icmp <-chan Msg, udp <-chan time.Duration, o Options) []Hop {
	var hops []Hop
	silent := 0
	for ttl := 1; ttl <= o.MaxTTL; ttl++ {
		for len(icmp) > 0 { // хвосты прошлого шага
			<-icmp
		}
		for len(udp) > 0 {
			<-udp
		}
		start := time.Now()
		port, err := s.Send(ttl)
		hop := Hop{TTL: ttl, Ms: -1}
		if err == nil {
			deadline := time.After(o.Wait)
		wait:
			for {
				select {
				case m := <-icmp:
					if m.OrigDst != dst || m.OrigSrcPort != port {
						continue
					}
					hop.IP, hop.Ms = m.From.String(), int(time.Since(start).Milliseconds())
					if m.Type == 3 && m.From == dst.Addr() {
						hop.Server = true
					}
					break wait
				case <-udp:
					hop.IP, hop.Ms, hop.Server = dst.Addr().String(), int(time.Since(start).Milliseconds()), true
					break wait
				case <-deadline:
					break wait
				}
			}
		}
		hops = append(hops, hop)
		if hop.Server {
			break
		}
		if hop.IP == "" {
			silent++
			if silent >= o.StopAfterSilent {
				break
			}
		} else {
			silent = 0
		}
	}
	return hops
}

// Reached — дошёл ли пакет до сервера игры (тот ответил).
func Reached(hops []Hop) bool {
	for _, h := range hops {
		if h.Server {
			return true
		}
	}
	return false
}

// LastHop — последний узел, который ответил (до тишины).
func LastHop(hops []Hop) (Hop, bool) {
	for i := len(hops) - 1; i >= 0; i-- {
		if hops[i].IP != "" {
			return hops[i], true
		}
	}
	return Hop{}, false
}
