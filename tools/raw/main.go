package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"albionzonefix/internal/game"
	"albionzonefix/internal/record"
)

func main() {
	f, _ := os.Open(os.Args[1])
	var t0 time.Time
	record.Read(f, func(p game.Packet) {
		if t0.IsZero() {
			t0 = p.T
		}
		if !strings.Contains(p.Addr, os.Args[2]) {
			return
		}
		d := "←"
		if p.Out {
			d = "→"
		}
		h := hex.EncodeToString(p.Payload)
		if len(h) > 64 {
			h = h[:64] + "…"
		}
		cmds := ""
		if len(p.Payload) >= 12 {
			cmds = fmt.Sprintf("flags=%d cmds=%d", p.Payload[2], p.Payload[3])
			if len(p.Payload) >= 13 {
				cmds += fmt.Sprintf(" cmd0type=%d", p.Payload[12])
			}
		}
		fmt.Printf("%7.2f %s %4d байт %-28s %s\n", p.T.Sub(t0).Seconds(), d, len(p.Payload), cmds, h)
	})
}
