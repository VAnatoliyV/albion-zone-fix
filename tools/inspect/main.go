// inspect печатает, что внутри записи .azf: серверы, время, операции Photon.
package main

import (
	"fmt"
	"os"
	"sort"
	"time"

	"albionzonefix/internal/game"
	"albionzonefix/internal/photon"
	"albionzonefix/internal/record"
)

func op(code byte, p map[byte]interface{}) string {
	if v, ok := p[253]; ok {
		return fmt.Sprintf("%v", v)
	}
	return fmt.Sprintf("b%d", code)
}

func short(p map[byte]interface{}) string {
	keys := []int{}
	for k := range p {
		keys = append(keys, int(k))
	}
	sort.Ints(keys)
	s := ""
	for _, k := range keys {
		if k == 253 {
			continue
		}
		v := fmt.Sprintf("%v", p[byte(k)])
		if len(v) > 40 {
			v = v[:40] + "…"
		}
		s += fmt.Sprintf(" %d=%s", k, v)
	}
	return s
}

func main() {
	f, _ := os.Open(os.Args[1])
	mode := "ops"
	if len(os.Args) > 2 {
		mode = os.Args[2]
	}
	var t0 time.Time
	parsers := map[string]*photon.PhotonParser{}
	var cur game.Packet
	rel := func() string { return fmt.Sprintf("%7.2f", cur.T.Sub(t0).Seconds()) }
	servers := map[string]int{}
	first, last := map[string]float64{}, map[string]float64{}
	record.Read(f, func(p game.Packet) {
		if t0.IsZero() {
			t0 = p.T
		}
		cur = p
		servers[p.Addr]++
		s := p.T.Sub(t0).Seconds()
		if _, ok := first[p.Addr]; !ok {
			first[p.Addr] = s
		}
		last[p.Addr] = s
		key := p.Addr
		if p.Out {
			key = ">" + key
		}
		pr := parsers[key]
		if pr == nil {
			pr = photon.NewPhotonParser(
				func(c byte, pm map[byte]interface{}) {
					if mode == "ops" {
						fmt.Printf("%s REQ  %-22s op=%s%s\n", rel(), cur.Addr, op(c, pm), short(pm))
					}
				},
				func(c byte, rc int16, _ string, pm map[byte]interface{}) {
					if mode == "ops" {
						fmt.Printf("%s RESP %-22s op=%s rc=%d%s\n", rel(), cur.Addr, op(c, pm), rc, short(pm))
					}
				},
				func(c byte, pm map[byte]interface{}) {
					if mode == "events" {
						fmt.Printf("%s EV   %-22s code=%d ev252=%v%s\n", rel(), cur.Addr, c, pm[252], short(pm))
					}
				})
			parsers[key] = pr
		}
		func() { defer func() { recover() }(); pr.ReceivePacket(p.Payload) }()
	})
	if mode == "servers" {
		for a, n := range servers {
			fmt.Printf("%-24s пакетов %5d  с %7.1f по %7.1f с\n", a, n, first[a], last[a])
		}
	}
}
