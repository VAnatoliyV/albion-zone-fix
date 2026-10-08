package update

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for n, c := range files {
		p := filepath.Join(root, filepath.FromSlash(n))
		os.MkdirAll(filepath.Dir(p), 0755)
		if err := os.WriteFile(p, []byte(c), 0755); err != nil {
			t.Fatal(err)
		}
	}
}

func read(t *testing.T, root, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		return "<нет>"
	}
	return string(b)
}

type fakeEnv struct {
	waited, stopped int
	unblocked       []string
	started         []string
	log             []string
}

func (f *fakeEnv) env(exitOK bool) Env {
	return Env{
		WaitExit:     func(pid int, d time.Duration) bool { f.waited++; return exitOK },
		StopReceiver: func() { f.stopped++ },
		Unblock:      func(p string) { f.unblocked = append(f.unblocked, filepath.Base(p)) },
		Start:        func(exe string) error { f.started = append(f.started, exe); return nil },
		Logf:         func(format string, a ...any) { f.log = append(f.log, format) },
	}
}

// Папка программы версии 1.0.0 и распакованная 1.0.1.
func fixture(t *testing.T) Plan {
	dir := t.TempDir()
	p := Plan{Src: filepath.Join(dir, "ставлю", "AlbionJournal"), Dest: filepath.Join(dir, "Программа"),
		Prev: filepath.Join(dir, "предыдущая"), PID: 4242, DataDir: dir}
	write(t, p.Dest, map[string]string{
		"AlbionJournal.exe":          "1.0.0",
		"acp-prices.exe":             "приёмник 1",
		"zapret/bin/WinDivert64.sys": "драйвер",
		"README-RU.txt":              "старое",
		"мои-заметки.txt":            "своё",
	})
	write(t, p.Src, map[string]string{
		"AlbionJournal.exe":          "1.0.1",
		"acp-prices.exe":             "приёмник 2",
		"zapret/bin/WinDivert64.sys": "драйвер", // тот же — не трогаем
		"README-RU.txt":              "новое",
		"новая/папка/файл.txt":       "свежий",
	})
	// Старый «предыдущая» от прошлого раза убирается.
	write(t, p.Prev, map[string]string{"старьё.exe": "x"})
	return p
}

func TestApplyReplacesAndKeepsPrevious(t *testing.T) {
	p := fixture(t)
	p.Restart = true
	f := &fakeEnv{}
	if err := Apply(p, f.env(true)); err != nil {
		t.Fatal(err)
	}
	for n, want := range map[string]string{
		"AlbionJournal.exe": "1.0.1", "acp-prices.exe": "приёмник 2", "README-RU.txt": "новое",
		"zapret/bin/WinDivert64.sys": "драйвер", "мои-заметки.txt": "своё", "новая/папка/файл.txt": "свежий",
	} {
		if got := read(t, p.Dest, n); got != want {
			t.Errorf("%s = %q, ждал %q", n, got, want)
		}
	}
	for n, want := range map[string]string{"AlbionJournal.exe": "1.0.0", "acp-prices.exe": "приёмник 1", "README-RU.txt": "старое"} {
		if got := read(t, p.Prev, n); got != want {
			t.Errorf("предыдущая/%s = %q, ждал %q", n, got, want)
		}
	}
	if read(t, p.Prev, "zapret/bin/WinDivert64.sys") != "<нет>" {
		t.Error("одинаковый драйвер трогать не надо")
	}
	if read(t, p.Prev, "старьё.exe") != "<нет>" {
		t.Error("прошлая «предыдущая» не убрана")
	}
	if f.waited != 1 || f.stopped != 1 {
		t.Errorf("ждал %d, гасил приёмник %d", f.waited, f.stopped)
	}
	if len(f.started) != 1 || f.started[0] != filepath.Join(p.Dest, MainExe) {
		t.Errorf("перезапуск: %v", f.started)
	}
	if !strings.Contains(strings.Join(f.unblocked, ","), MainExe) || len(f.unblocked) != 4 {
		t.Errorf("Mark-of-the-Web снят с %v", f.unblocked)
	}
	if _, err := os.Stat(filepath.Join(p.Dest, MainExe+".new")); !os.IsNotExist(err) {
		t.Error("временный .new остался")
	}
}

func TestApplyWaitsForExit(t *testing.T) {
	p := fixture(t)
	p.Restart = true
	f := &fakeEnv{}
	if err := Apply(p, f.env(false)); err == nil {
		t.Fatal("программа не закрылась — ставить нельзя")
	}
	if read(t, p.Dest, MainExe) != "1.0.0" || f.stopped != 0 || len(f.started) != 0 {
		t.Fatal("при живой программе ничего трогать нельзя")
	}
}

func TestApplyRollsBack(t *testing.T) {
	p := fixture(t)
	p.Restart = true
	f := &fakeEnv{}
	e := f.env(true)
	// Копирование ломается на README (после того как exe уже заменены).
	e.Copy = func(src, dst string) error {
		if strings.Contains(src, "README") && strings.HasPrefix(src, p.Src) {
			return errors.New("диск полон")
		}
		return copyFile(src, dst)
	}
	if err := Apply(p, e); err == nil {
		t.Fatal("ждал ошибку")
	}
	for n, want := range map[string]string{
		"AlbionJournal.exe": "1.0.0", "acp-prices.exe": "приёмник 1", "README-RU.txt": "старое",
		"zapret/bin/WinDivert64.sys": "драйвер", "мои-заметки.txt": "своё", "новая/папка/файл.txt": "<нет>",
	} {
		if got := read(t, p.Dest, n); got != want {
			t.Errorf("после отката %s = %q, ждал %q", n, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(p.Dest, "новая")); !os.IsNotExist(err) {
		t.Error("созданная папка осталась после отката")
	}
	// Перезапуск всё равно — прежней версии.
	if len(f.started) != 1 {
		t.Errorf("после отката прежняя версия должна запуститься: %v", f.started)
	}
}

func TestApplyNeedsProgram(t *testing.T) {
	p := fixture(t)
	os.Remove(filepath.Join(p.Src, ReceiverExe))
	f := &fakeEnv{}
	if err := Apply(p, f.env(true)); err == nil || read(t, p.Dest, MainExe) != "1.0.0" {
		t.Fatal("неполное обновление не ставим")
	}
}

func TestPlanArgs(t *testing.T) {
	p := Plan{Src: `C:\a b\src`, Dest: `D:\AJ`, Prev: `C:\p`, PID: 7, Restart: true, DataDir: `C:\data`}
	got := strings.Join(p.Args(), "|")
	want := `-apply-update|C:\a b\src|-target|D:\AJ|-prev|C:\p|-pid|7|-data|C:\data|-restart`
	if got != want {
		t.Fatalf("%s", got)
	}
}
