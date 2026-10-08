package update

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// GitHub понарошку: API, zip и подпись.
type fakeGH struct {
	srv      *httptest.Server
	tag      string
	zip      []byte
	sig      string
	api, dl  atomic.Int32
	noAssets bool
}

// signed — версия, под которой подписан zip (обычно равна тегу).
func newGH(t *testing.T, k ed25519.PrivateKey, tag, signed string) *fakeGH {
	t.Helper()
	g := &fakeGH{tag: tag}
	dir := t.TempDir()
	z := filepath.Join(dir, ZipName)
	makeZip(t, z, map[string]string{
		"AlbionJournal/AlbionJournal.exe": "exe",
		"AlbionJournal/acp-prices.exe":    "приёмник",
		"AlbionJournal/README-RU.txt":     "читай",
	})
	g.zip, _ = os.ReadFile(z)
	if signed == "x" {
		signed = "9.9.9"
	}
	g.sig, _ = Sign(k, z, signed)
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api":
			g.api.Add(1)
			var as []map[string]string
			if !g.noAssets {
				as = []map[string]string{
					{"name": ZipName, "browser_download_url": g.srv.URL + "/dl/" + ZipName},
					{"name": SigName, "browser_download_url": g.srv.URL + "/dl/" + SigName},
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"tag_name": g.tag, "body": "что нового", "assets": as})
		case "/dl/" + ZipName:
			g.dl.Add(1)
			w.Write(g.zip)
		case "/dl/" + SigName:
			w.Write([]byte(g.sig))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(g.srv.Close)
	return g
}

type harness struct {
	u        *Updater
	dir      string
	prog     string
	launched [][]string
	writable bool
	now      time.Time
	logs     []string
}

func newHarness(t *testing.T, g *fakeGH, pub ed25519.PublicKey, current string) *harness {
	h := &harness{dir: t.TempDir(), writable: true, now: time.Unix(1_800_000_000, 0)}
	h.prog = filepath.Join(h.dir, "Программа")
	os.MkdirAll(h.prog, 0755)
	h.u = New(Config{
		Dir: filepath.Join(h.dir, "обновление"), Current: current, ProgramDir: h.prog, DataDir: h.dir, PID: 99,
		PublicKey: pub, APIURL: g.srv.URL + "/api", Client: g.srv.Client(),
		AllowURL: func(s string) bool { return strings.HasPrefix(s, g.srv.URL+"/") },
		Launch: func(exe string, args ...string) error {
			h.launched = append(h.launched, append([]string{exe}, args...))
			return nil
		},
		Writable: func(string) bool { return h.writable },
		TempDir:  filepath.Join(h.dir, "temp"),
		Now:      func() time.Time { return h.now },
		Logf:     func(f string, a ...any) { h.logs = append(h.logs, f) },
	})
	return h
}

func TestUpdaterDownloadsVerifiesAndInstalls(t *testing.T) {
	k := testKey(t)
	g := newGH(t, k, "v1.0.1", "1.0.1")
	h := newHarness(t, g, k.Public().(ed25519.PublicKey), "1.0.0")
	h.u.CheckSync()
	s := h.u.Status()
	if s.State != StateReady || s.Ready == nil || s.Ready.Version != "1.0.1" || s.Ready.Notes != "что нового" || s.NoWrite {
		t.Fatalf("%+v %v", s, h.logs)
	}
	// Повторная проверка не качает заново.
	h.u.CheckSync()
	if g.dl.Load() != 1 || h.u.Status().State != StateReady {
		t.Fatalf("скачано %d раз", g.dl.Load())
	}
	if err := h.u.Install(true); err != nil {
		t.Fatal(err)
	}
	if len(h.launched) != 1 {
		t.Fatal("установщик не запущен")
	}
	l := h.launched[0]
	stage := filepath.Join(h.dir, "обновление", "ставлю", "AlbionJournal")
	if l[0] != filepath.Join(stage, MainExe) {
		t.Fatalf("установщик не из распакованной новой версии: %s", l[0])
	}
	want := Plan{Src: stage, Dest: h.prog, Prev: filepath.Join(h.dir, "обновление", "предыдущая"), PID: 99, Restart: true, DataDir: h.dir}.Args()
	if strings.Join(l[1:], "|") != strings.Join(want, "|") {
		t.Fatalf("аргументы %v", l[1:])
	}
	if !h.u.Status().Installing || h.u.Install(false) == nil {
		t.Fatal("вторая установка не нужна")
	}
}

func TestUpdaterRejectsBadSignature(t *testing.T) {
	k := testKey(t)
	g := newGH(t, testKey(t), "v1.0.1", "1.0.1") // подписано чужим ключом
	h := newHarness(t, g, k.Public().(ed25519.PublicKey), "1.0.0")
	h.u.CheckSync()
	if s := h.u.Status(); s.State != StateError || s.Ready != nil {
		t.Fatalf("%+v", s)
	}
	for _, n := range []string{zipFile, sigFile, checkDir, metaFile} {
		if _, err := os.Stat(filepath.Join(h.dir, "обновление", n)); !os.IsNotExist(err) {
			t.Errorf("%s не удалён после неверной подписи", n)
		}
	}
	if !strings.Contains(strings.Join(h.logs, "\n"), "отвергнуто") {
		t.Errorf("в журнале нет причины: %v", h.logs)
	}
	if h.u.Install(true) == nil {
		t.Fatal("ставить нечего")
	}
}

func TestUpdaterRejectsVersionMismatch(t *testing.T) {
	k := testKey(t)
	// Старый подписанный zip (подпись над версией 1.0.1) выложен под новым тегом.
	g := newGH(t, k, "v1.0.2", "1.0.1")
	h := newHarness(t, g, k.Public().(ed25519.PublicKey), "1.0.0")
	h.u.CheckSync()
	if s := h.u.Status(); s.State != StateError || s.Ready != nil {
		t.Fatalf("%+v", s)
	}
}

func TestUpdaterLatestAndOldZoneFix(t *testing.T) {
	k := testKey(t)
	for _, tag := range []string{"v1.0.0", "v0.3.0", "v0.9.9"} {
		g := newGH(t, k, tag, "x")
		g.noAssets = tag != "v1.0.0"
		h := newHarness(t, g, k.Public().(ed25519.PublicKey), "1.0.0")
		h.u.CheckSync()
		if s := h.u.Status(); s.State != StateLatest || s.Version != "1.0.0" || g.dl.Load() != 0 {
			t.Fatalf("%s: %+v", tag, s)
		}
	}
}

func TestUpdaterNewerWithoutAssets(t *testing.T) {
	k := testKey(t)
	g := newGH(t, k, "v1.1.0", "1.1.0")
	g.noAssets = true
	h := newHarness(t, g, k.Public().(ed25519.PublicKey), "1.0.0")
	h.u.CheckSync()
	if s := h.u.Status(); s.State != StateError {
		t.Fatalf("%+v", s)
	}
}

func TestUpdaterTickIntervalAndPickup(t *testing.T) {
	k := testKey(t)
	g := newGH(t, k, "v1.0.1", "1.0.1")
	pub := k.Public().(ed25519.PublicKey)
	h := newHarness(t, g, pub, "1.0.0")
	h.u.Tick()
	if g.api.Load() != 1 || h.u.Status().State != StateReady {
		t.Fatal("первая проверка")
	}
	// «Перезапуск программы» через 10 минут: сеть не трогаем, скачанное подхватываем.
	u2 := New(h.u.c)
	h.now = h.now.Add(10 * time.Minute)
	u2.Tick()
	if g.api.Load() != 1 {
		t.Fatal("чаще раза в час ходить нельзя")
	}
	if s := u2.Status(); s.State != StateReady || s.Ready == nil || s.Ready.Version != "1.0.1" {
		t.Fatalf("скачанное не подхвачено: %+v", s)
	}
	// Через час — снова спрашиваем.
	h.now = h.now.Add(time.Hour)
	u2.Tick()
	if g.api.Load() != 2 || g.dl.Load() != 1 {
		t.Fatalf("api %d, загрузок %d", g.api.Load(), g.dl.Load())
	}
	// Автообновление выключено — таймер молчит, кнопка работает.
	h.now = h.now.Add(2 * time.Hour)
	c := h.u.c
	c.Auto = func() bool { return false }
	u3 := New(c)
	u3.Tick()
	if g.api.Load() != 2 {
		t.Fatal("выключено — по таймеру не ходим")
	}
	u3.CheckSync()
	if g.api.Load() != 3 {
		t.Fatal("кнопка проверяет всегда")
	}
}

func TestUpdaterPickupRejectsTampered(t *testing.T) {
	k := testKey(t)
	g := newGH(t, k, "v1.0.1", "1.0.1")
	h := newHarness(t, g, k.Public().(ed25519.PublicKey), "1.0.0")
	h.u.CheckSync()
	// Пока программа не работала, zip в каталоге данных подменили.
	os.WriteFile(filepath.Join(h.dir, "обновление", zipFile), []byte("подмена"), 0644)
	u2 := New(h.u.c)
	h.now = h.now.Add(time.Minute)
	u2.Tick()
	if u2.Status().Ready != nil {
		t.Fatal("подменённый zip подхвачен")
	}
}

func TestUpdaterInstallChecksAgain(t *testing.T) {
	k := testKey(t)
	g := newGH(t, k, "v1.0.1", "1.0.1")
	h := newHarness(t, g, k.Public().(ed25519.PublicKey), "1.0.0")
	h.u.CheckSync()
	os.WriteFile(filepath.Join(h.dir, "обновление", zipFile), []byte("подмена"), 0644)
	if err := h.u.Install(true); err == nil || len(h.launched) != 0 {
		t.Fatal("подменённое после проверки не ставим")
	}
	if s := h.u.Status(); !s.InstallFailed || s.Ready != nil {
		t.Fatalf("%+v", s)
	}
}

func TestUpdaterInstallNoWriteAndTemp(t *testing.T) {
	k := testKey(t)
	g := newGH(t, k, "v1.0.1", "1.0.1")
	h := newHarness(t, g, k.Public().(ed25519.PublicKey), "1.0.0")
	h.writable = false
	h.u.CheckSync()
	if s := h.u.Status(); s.State != StateReady || !s.NoWrite {
		t.Fatalf("нет прав должно быть видно сразу: %+v", s)
	}
	if err := h.u.Install(true); err == nil || len(h.launched) != 0 {
		t.Fatal("без прав не ставим")
	}
	// Запущено из временной папки (двойной щелчок прямо в zip).
	h.writable = true
	c := h.u.c
	c.ProgramDir = filepath.Join(c.TempDir, "Temp1_AlbionJournal.zip", "AlbionJournal")
	u2 := New(c)
	u2.CheckSync()
	if err := u2.Install(false); err == nil || len(h.launched) != 0 {
		t.Fatal("из временной папки не ставим")
	}
}

func TestUpdaterLaunchFails(t *testing.T) {
	k := testKey(t)
	g := newGH(t, k, "v1.0.1", "1.0.1")
	h := newHarness(t, g, k.Public().(ed25519.PublicKey), "1.0.0")
	c := h.u.c
	c.Launch = func(string, ...string) error { return errors.New("нельзя") }
	u := New(c)
	u.CheckSync()
	if err := u.Install(true); err == nil {
		t.Fatal("ждал ошибку")
	}
	if s := u.Status(); !s.InstallFailed || s.Installing || s.Ready == nil {
		t.Fatalf("%+v", s)
	}
}

func TestUpdaterDev(t *testing.T) {
	k := testKey(t)
	g := newGH(t, k, "v1.0.1", "1.0.1")
	h := newHarness(t, g, k.Public().(ed25519.PublicKey), "dev")
	h.u.Tick()
	h.u.CheckSync()
	if h.u.Status().State != StateDev || g.api.Load() != 0 {
		t.Fatal("сборка без версии не обновляется")
	}
}

func TestInside(t *testing.T) {
	if !inside(filepath.Join("/tmp", "a", "b"), "/tmp") || !inside("/TMP/a", "/tmp") || inside("/tmpx/a", "/tmp") || inside("/other", "/tmp") {
		t.Fatal("inside")
	}
}

func TestUpdaterCleansAfterInstalled(t *testing.T) {
	k := testKey(t)
	g := newGH(t, k, "v1.0.1", "1.0.1")
	h := newHarness(t, g, k.Public().(ed25519.PublicKey), "1.0.0")
	h.u.CheckSync()
	// Новая копия (1.0.1) запустилась через минуту после установки.
	c := h.u.c
	c.Current = "1.0.1"
	u2 := New(c)
	h.now = h.now.Add(time.Minute)
	u2.Tick()
	if u2.Status().Ready != nil {
		t.Fatal("своя же версия — не обновление")
	}
	if _, err := os.Stat(filepath.Join(h.dir, "обновление", zipFile)); !os.IsNotExist(err) {
		t.Fatal("скачанный zip уже стоящей версии не убран")
	}
}
