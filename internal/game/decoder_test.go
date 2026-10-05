package game

import (
	"encoding/binary"
	"testing"
	"time"

	"albionzonefix/internal/photon"
)

// Пакет Photon с одной надёжной командой — как в тестах форка albiondata-client.
func pkt(msgType, opCode byte, payload []byte) []byte {
	hdr := []byte{0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0}
	data := append([]byte{0x00, msgType, opCode}, payload...)
	cmd := make([]byte, 12)
	cmd[0] = 6
	binary.BigEndian.PutUint32(cmd[4:], uint32(12+len(data)))
	return append(append(hdr, cmd...), data...)
}

// Таблица параметров: 253 = код операции (Int1), опционально 8 = строка.
func params(op byte, loc string) []byte {
	n := byte(1)
	if loc != "" {
		n = 2
	}
	b := []byte{n, 253, 11, op}
	if loc != "" {
		b = append(b, 8, photon.TypeString, byte(len(loc)))
		b = append(b, loc...)
	}
	return b
}

func response(op byte, loc string) []byte {
	body := []byte{0, 0, photon.TypeNull} // код возврата 0, без отладочной строки
	return pkt(photon.MsgResponse, 1, append(body, params(op, loc)...))
}

func TestDecoderSeesZoneChange(t *testing.T) {
	var got []Ev
	d := NewDecoder(func(e Ev) { got = append(got, e) })
	t0 := time.Unix(1000, 0)

	d.Feed(Packet{T: t0, Out: true, Addr: "5.188.125.10:5056", Payload: pkt(photon.MsgRequest, 1, params(OpChangeCluster, ""))})
	d.Feed(Packet{T: t0.Add(3 * time.Second), Out: false, Addr: "5.188.125.22:5056", Payload: response(OpJoin, "4002")})
	d.Feed(Packet{T: t0.Add(4 * time.Second), Out: false, Addr: "5.188.125.22:5056", Payload: pkt(photon.MsgEvent, 29, []byte{0})})

	if len(got) != 3 {
		t.Fatalf("событий %d, ждали 3: %+v", len(got), got)
	}
	if got[0].Kind != ChangeCluster || got[0].Server != "5.188.125.10:5056" {
		t.Errorf("первое: %+v", got[0])
	}
	if got[1].Kind != Join || got[1].Location != "4002" || got[1].Server != "5.188.125.22:5056" {
		t.Errorf("второе: %+v", got[1])
	}
	if got[2].Kind != Incoming || !got[2].T.Equal(t0.Add(4*time.Second)) {
		t.Errorf("третье: %+v", got[2])
	}
}

func TestDecoderIgnoresGarbageAndOtherOps(t *testing.T) {
	var got []Ev
	d := NewDecoder(func(e Ev) { got = append(got, e) })
	d.Feed(Packet{T: time.Unix(1, 0), Addr: "1.1.1.1:5056", Payload: []byte{1, 2, 3}})
	d.Feed(Packet{T: time.Unix(2, 0), Out: true, Addr: "1.1.1.1:5056", Payload: pkt(photon.MsgRequest, 1, params(77, ""))})
	d.Feed(Packet{T: time.Unix(3, 0), Addr: "1.1.1.1:5056", Payload: response(OpJoin, "@@мусор")})
	if len(got) != 0 {
		t.Fatalf("ждали тишину, получили %+v", got)
	}
}

func TestNormalizeLocation(t *testing.T) {
	cases := map[string]string{"4002": "4002", " 0301.": "0301", "BLACKBANK-2311": "BLACKBANK-2311", "xx": "", "3004-HellDen": "3004-HellDen"}
	for in, want := range cases {
		if got := NormalizeLocation(in); got != want {
			t.Errorf("%q → %q, ждали %q", in, got, want)
		}
	}
}
