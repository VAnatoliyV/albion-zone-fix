// Пакет sniff читает UDP-пакеты Albion через драйвер WinDivert (только Windows)
// и отдаёт их в виде game.Packet.
package sniff

import (
	"net"
	"strconv"
)

// ParseIP достаёт из сырого IP-пакета удалённый адрес (сервер игры) и нагрузку UDP.
// outbound — пакет идёт от нас, значит удалённый адрес это получатель.
func ParseIP(b []byte, outbound bool) (addr string, payload []byte, ok bool) {
	if len(b) < 1 {
		return "", nil, false
	}
	var src, dst net.IP
	var udp []byte
	switch b[0] >> 4 {
	case 4:
		if len(b) < 20 {
			return "", nil, false
		}
		ihl := int(b[0]&0x0F) * 4
		if ihl < 20 || len(b) < ihl+8 || b[9] != 17 {
			return "", nil, false
		}
		src, dst, udp = net.IP(b[12:16]), net.IP(b[16:20]), b[ihl:]
	case 6:
		if len(b) < 48 || b[6] != 17 { // заголовки-расширения не разбираем: игра их не шлёт
			return "", nil, false
		}
		src, dst, udp = net.IP(b[8:24]), net.IP(b[24:40]), b[40:]
	default:
		return "", nil, false
	}
	sport := int(udp[0])<<8 | int(udp[1])
	dport := int(udp[2])<<8 | int(udp[3])
	ip, port := src, sport
	if outbound {
		ip, port = dst, dport
	}
	return net.JoinHostPort(ip.String(), strconv.Itoa(port)), udp[8:], true
}

// CollectorPort — порт, который слушает сборщик цен (его pcap-фильтр:
// "tcp port 5056 || udp port 5056"). 5055 — сервер входа, его сборщик не видит.
const CollectorPort = 5056

// ForCollector — нужен ли пакет разборщику сборщика: IPv4, UDP или TCP,
// порт 5056 с любой стороны. IPv6 сборщик не разбирает.
func ForCollector(b []byte) bool {
	if len(b) < 20 || b[0]>>4 != 4 {
		return false
	}
	ihl := int(b[0]&0x0F) * 4
	if ihl < 20 || len(b) < ihl+4 || (b[9] != 17 && b[9] != 6) {
		return false
	}
	sport := int(b[ihl])<<8 | int(b[ihl+1])
	dport := int(b[ihl+2])<<8 | int(b[ihl+3])
	return sport == CollectorPort || dport == CollectorPort
}
