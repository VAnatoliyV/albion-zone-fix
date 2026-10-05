package record

import (
	"bytes"
	"testing"
	"time"

	"albionzonefix/internal/game"
)

func TestRoundTrip(t *testing.T) {
	in := []game.Packet{
		{T: time.Unix(100, 5), Out: true, Addr: "5.188.125.10:5056", Payload: []byte{1, 2, 3}},
		{T: time.Unix(101, 0), Out: false, Addr: "[2a00::1]:5055", Payload: []byte{}},
	}
	var buf bytes.Buffer
	w, err := NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range in {
		if err := w.Write(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	var out []game.Packet
	if err := Read(&buf, func(p game.Packet) { out = append(out, p) }); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || !out[0].T.Equal(in[0].T) || !out[0].Out || out[0].Addr != in[0].Addr ||
		!bytes.Equal(out[0].Payload, in[0].Payload) || out[1].Addr != in[1].Addr || out[1].Out {
		t.Fatalf("прочитали не то: %+v", out)
	}
}

func TestRejectsForeignFile(t *testing.T) {
	if err := Read(bytes.NewReader([]byte("PK\x03\x04 это zip")), func(game.Packet) {}); err == nil {
		t.Fatal("чужой файл принят")
	}
}
