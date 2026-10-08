package desktop

import (
	"encoding/binary"
	"testing"
)

func TestIconIsICOWithAllSizes(t *testing.T) {
	if len(Icon) < 6 || binary.LittleEndian.Uint16(Icon[2:]) != 1 {
		t.Fatal("не .ico")
	}
	n := int(binary.LittleEndian.Uint16(Icon[4:]))
	sizes := map[int]bool{}
	for i := 0; i < n; i++ {
		w := int(Icon[6+16*i])
		if w == 0 {
			w = 256
		}
		sizes[w] = true
	}
	for _, s := range []int{16, 32, 48, 256} {
		if !sizes[s] {
			t.Errorf("нет размера %d (трей, заголовок, Проводник)", s)
		}
	}
}

func TestQuitBeforeRun(t *testing.T) {
	d := New(Config{URL: "http://127.0.0.1:1/", Hidden: true})
	d.Quit()
	d.Run() // не должен ждать
}
