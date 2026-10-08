package app

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"albionzonefix/internal/collector"
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

func change() []byte { return pkt(photon.MsgRequest, 1, []byte{1, 253, 11, game.OpChangeCluster}) }
func join(loc string) []byte {
	b := []byte{0, 0, photon.TypeNull, 2, 253, 11, game.OpJoin, 8, photon.TypeString, byte(len(loc))}
	return pkt(photon.MsgResponse, 1, append(b, loc...))
}

func feedTransition(a *App, t time.Time, from, to string) {
	a.Feed(game.Packet{T: t, Out: true, Addr: "1.1.1.1:5056", Payload: change()})
	a.Feed(game.Packet{T: t.Add(2 * time.Second), Addr: "2.2.2.2:5056", Payload: join(to)})
	a.Feed(game.Packet{T: t.Add(3 * time.Second), Addr: "2.2.2.2:5056", Payload: pkt(photon.MsgEvent, 1, []byte{0})})
}

func TestTransitionsAreSavedAndReloaded(t *testing.T) {
	dir := t.TempDir()
	a := New(dir, dir, map[string]string{"0000": "Thetford", "4002": "Swamp Road"})
	t0 := time.Unix(5000, 0)
	a.Feed(game.Packet{T: t0, Addr: "1.1.1.1:5056", Payload: join("0000")})
	feedTransition(a, t0.Add(10*time.Second), "0000", "4002")

	st := a.State()
	if len(st.Recent) != 1 || st.Recent[0].ToName != "Swamp Road" || st.Recent[0].LoadSec != 2 || st.Zone != "Swamp Road" {
		t.Fatalf("состояние: %+v", st)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "переходы.jsonl"))
	if strings.Count(string(b), "\n") != 1 || !strings.Contains(string(b), `"to":"4002"`) {
		t.Fatalf("файл: %q", b)
	}
	b2 := New(dir, dir, nil)
	if len(b2.State().Recent) != 1 {
		t.Fatal("после перезапуска история пропала")
	}
}

func TestRecordingWritesPackets(t *testing.T) {
	dir := t.TempDir()
	a := New(dir, dir, nil)
	if err := a.StartRecord(time.Minute); err != nil {
		t.Fatal(err)
	}
	a.Feed(game.Packet{T: time.Now(), Addr: "1.1.1.1:5056", Payload: join("0000")})
	path := a.State().Recording
	a.StopRecord()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	n := 0
	if err := record.Read(f, func(game.Packet) { n++ }); err != nil || n != 1 {
		t.Fatalf("в записи %d пакетов, ошибка %v", n, err)
	}
}

func TestBypassWithoutWinwsReportsError(t *testing.T) {
	dir := t.TempDir()
	a := New(dir, dir, nil)
	if err := a.SetBypass(a.State().Strategies[0]); err != ErrBypassDisabled {
		t.Fatalf("встроенный обход по умолчанию запрещён, а получили %v", err)
	}
	s := a.Settings()
	s.BuiltinBypass = true
	a.SetSettings(s)
	if err := a.SetBypass(a.State().Strategies[0]); err == nil || err == ErrBypassDisabled {
		t.Fatalf("без winws включилось или не пустило: %v", err)
	}
	if a.State().Bypass != "off" {
		t.Fatal("состояние врёт")
	}
}

type fakeCollector struct {
	applied []collector.Config
	resets  int
}

func (f *fakeCollector) Apply(c collector.Config) error { f.applied = append(f.applied, c); return nil }
func (f *fakeCollector) ResetSession()                  { f.resets++ }
func (f *fakeCollector) Stats() collector.Stats {
	return collector.Stats{Running: len(f.applied) > 0 && f.applied[len(f.applied)-1].Running()}
}

func TestCollectorFollowsSettings(t *testing.T) {
	dir := t.TempDir()
	a := New(dir, dir, nil)
	if err := a.ResetSession(); err == nil {
		t.Fatal("сброс без счётчика должен вернуть ошибку")
	}
	f := &fakeCollector{}
	if err := a.AttachCollector(f); err != nil {
		t.Fatal(err)
	}
	if got := f.applied[0]; got != (collector.Config{Prices: true, ShareADP: true, Session: true}) {
		t.Fatalf("по умолчанию: %+v", got)
	}

	s := a.Settings()
	s.ShareADP = false
	if err := a.SetSettings(s); err != nil {
		t.Fatal(err)
	}
	if got := f.applied[1]; got.ShareADP || !got.Prices {
		t.Fatalf("ADP не выключился: %+v", got)
	}
	if b := New(dir, dir, nil); b.Settings().ShareADP {
		t.Fatal("настройка не сохранилась в файл")
	}

	a.SetCollecting(false)
	if got := f.applied[2]; got.Prices || !got.Session {
		t.Fatalf("сбор не остановился или счётчик выключился: %+v", got)
	}
	st := a.State()
	if st.Collecting || st.Settings.ShareADP || !st.Collector.Running || st.DataDir != dir {
		t.Fatalf("состояние: %+v", st)
	}

	a.ResetSession()
	if f.resets != 1 {
		t.Fatal("сброс не дошёл")
	}
	a.Shutdown()
	if got := f.applied[len(f.applied)-1]; got.Running() {
		t.Fatal("при выходе разборщик не остановлен")
	}
}

func TestCollectOnStartOff(t *testing.T) {
	dir := t.TempDir()
	a := New(dir, dir, nil)
	s := a.Settings()
	s.CollectOnStart = false
	a.SetSettings(s)
	b := New(dir, dir, nil)
	f := &fakeCollector{}
	b.AttachCollector(f)
	if b.Collecting() || f.applied[0].Prices || !f.applied[0].Session {
		t.Fatalf("сбор начался сам: %+v", f.applied)
	}
}
