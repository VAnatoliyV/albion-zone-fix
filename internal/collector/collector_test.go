package collector

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	client "github.com/ao-data/albiondata-client/client"
)

func TestEmbedConfigMapping(t *testing.T) {
	cases := []struct {
		cfg             Config
		public, private string
	}{
		{Config{Prices: true, ShareADP: true}, "share", ReceiverURL},
		{Config{Prices: true, ShareADP: false}, "", ReceiverURL},
		{Config{Prices: false, ShareADP: true, Session: true}, "", client.DeadUploader},
	}
	for _, c := range cases {
		ec := EmbedConfig("d", "", nil, c.cfg)
		if (c.public == "share") != ec.SharePublic || ec.PrivateURLs != c.private || ec.Session != c.cfg.Session || ec.DataDir != "d" {
			t.Fatalf("%+v → %+v", c.cfg, ec)
		}
	}
	if (Config{}).Running() || !(Config{Session: true}).Running() || !(Config{Prices: true}).Running() {
		t.Fatal("Running")
	}
}

// fameEventIPv4 — пакет игрового сервера Европы с событием UpdateFame (82).
func fameEventIPv4(fame int64) []byte {
	u := uint64((fame << 1) ^ (fame >> 63))
	var zz []byte
	for u >= 0x80 {
		zz = append(zz, byte(u)|0x80)
		u >>= 7
	}
	zz = append(zz, byte(u))
	params := append([]byte{2, 252, 11, 82, 2, 10}, zz...)
	data := append([]byte{0x00, 4, 82}, params...)
	cmd := make([]byte, 12)
	cmd[0] = 6
	binary.BigEndian.PutUint32(cmd[4:], uint32(12+len(data)))
	photon := append(append([]byte{0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0}, cmd...), data...)

	b := make([]byte, 28+len(photon))
	b[0], b[9] = 0x45, 17
	copy(b[12:], []byte{193, 169, 238, 10, 192, 168, 0, 2})
	binary.BigEndian.PutUint16(b[20:], 5056)
	binary.BigEndian.PutUint16(b[22:], 50000)
	binary.BigEndian.PutUint16(b[24:], uint16(8+len(photon)))
	copy(b[28:], photon)
	return b
}

func TestFeedReachesForkSession(t *testing.T) {
	dir := t.TempDir()
	c := New(dir, "", nil)
	defer c.Close()

	c.Feed(fameEventIPv4(500 * 10000)) // не запущен — молча мимо
	if c.Stats().Fed != 0 {
		t.Fatal("пакет принят до запуска")
	}
	if err := c.Apply(Config{Session: true}); err != nil {
		t.Fatal(err)
	}
	before := client.SessionFame()
	c.Feed(fameEventIPv4(500 * 10000))
	deadline := time.Now().Add(3 * time.Second)
	for client.SessionFame()-before != 500 {
		if time.Now().After(deadline) {
			t.Fatalf("фейм не дошёл: %+v", c.Stats())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if st := c.Stats(); !st.Running || st.Fed != 1 {
		t.Fatalf("%+v", st)
	}

	c.ResetSession()
	if client.SessionFame() != 0 {
		t.Fatal("сброс сессии")
	}
	if _, err := os.Stat(filepath.Join(dir, "albion-session.json")); err != nil {
		t.Fatalf("сессия пишется не в каталог данных: %v", err)
	}

	if err := c.Apply(Config{}); err != nil || c.Stats().Running {
		t.Fatalf("не остановился: %v %+v", err, c.Stats())
	}
}

func TestFeedPanicIsCaught(t *testing.T) {
	old := feed
	t.Cleanup(func() { feed = old })
	feed = func([]byte) error { panic("проверка") }
	var log strings.Builder
	c := &Collector{log: &log}
	if err := c.feedOne([]byte{1}); err == nil {
		t.Fatal("паника не превратилась в ошибку")
	}
	st := c.Stats()
	if st.Panics < 1 || !strings.Contains(st.LastPanic, "проверка") || !strings.Contains(log.String(), "проверка") {
		t.Fatalf("паника не учтена: %+v, журнал %q", st, log.String())
	}
}
