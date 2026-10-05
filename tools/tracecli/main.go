// tracecli — трассировка до сервера Albion из командной строки (проверка вне Windows, нужен root).
package main

import (
	"fmt"
	"net/netip"
	"os"
	"time"

	"albionzonefix/internal/trace"
)

func main() {
	dst := netip.MustParseAddrPort(os.Args[1])
	icmp, closeICMP, err := trace.OpenICMP("")
	if err != nil {
		fmt.Println("ICMP:", err)
		return
	}
	defer closeICMP()
	s, err := trace.NewUDPSender(dst)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer s.Close()
	for _, h := range trace.Run(s, dst, icmp, s.UDP, trace.Options{MaxTTL: 30, Wait: 1500 * time.Millisecond, StopAfterSilent: 5}) {
		fmt.Printf("%2d  %-16s %5d мс  сервер=%v\n", h.TTL, h.IP, h.Ms, h.Server)
	}
}
