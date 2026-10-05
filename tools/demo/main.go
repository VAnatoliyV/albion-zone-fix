// demo пишет синтетическую запись .azf с переходами — проверить программу без игры.
package main

import (
	"encoding/binary"
	"os"
	"time"

	"albionzonefix/internal/game"
	"albionzonefix/internal/photon"
	"albionzonefix/internal/record"
)

func pkt(msgType, opCode byte, payload []byte) []byte {
	hdr := []byte{0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0}
	data := append([]byte{0x00, msgType, opCode}, payload...)
	cmd := make([]byte, 12)
	cmd[0] = 6
	binary.BigEndian.PutUint32(cmd[4:], uint32(12+len(data)))
	return append(append(hdr, cmd...), data...)
}

func main() {
	f, _ := os.Create(os.Args[1])
	defer f.Close()
	w, _ := record.NewWriter(f)
	defer w.Flush()
	t := time.Now().Add(-time.Hour)
	put := func(dt float64, out bool, addr string, pl []byte) {
		t = t.Add(time.Duration(dt * float64(time.Second)))
		w.Write(game.Packet{T: t, Out: out, Addr: addr, Payload: pl})
	}
	change := pkt(photon.MsgRequest, 1, []byte{1, 253, 11, game.OpChangeCluster})
	join := func(loc string) []byte {
		b := []byte{0, 0, photon.TypeNull, 2, 253, 11, game.OpJoin, 8, photon.TypeString, byte(len(loc))}
		return pkt(photon.MsgResponse, 1, append(b, loc...))
	}
	ev := pkt(photon.MsgEvent, 1, []byte{0})
	put(0, false, "5.188.125.1:5056", join("0000")) // вход в игру в Тетфорде
	route := []struct {
		to          string
		load, alive float64
	}{{"0004", 2.1, 0.4}, {"0301", 9.8, 6.5}, {"0000", 3.0, 0.2}}
	srv := 2
	for _, r := range route {
		put(30, true, "5.188.125.1:5056", change)
		srv++
		a := "5.188.125." + string(rune('0'+srv)) + ":5056"
		put(r.load, false, a, join(r.to))
		put(r.alive, false, a, ev)
	}
	put(30, true, "5.188.125.9:5056", change) // этот переход повиснет
	put(45, false, "5.188.125.9:5056", []byte{1, 2})
}
