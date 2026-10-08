package gameguard

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"albionzonefix/internal/autostart"
	"albionzonefix/internal/settings"
)

// fakeGuard: знак одной копии на всех и просьба закрыться.
type fakeGuard struct {
	held     *bool
	stop     func() bool
	waits    int
	released bool
}

func (g *fakeGuard) Wait(time.Duration) bool {
	g.waits++
	return g.stop != nil && g.stop()
}

func (g *fakeGuard) Release() { *g.held = false; g.released = true }

func acquirer(held *bool, g *fakeGuard) func() (Guard, bool) {
	return func() (Guard, bool) {
		if *held {
			return nil, false
		}
		*held = true
		g.held = held
		return g, true
	}
}

func snapshots(seq ...bool) (func() (bool, error), *int) {
	n := 0
	return func() (bool, error) {
		if n >= len(seq) {
			n++
			return seq[len(seq)-1], nil
		}
		n++
		return seq[n-1], nil
	}, &n
}

// Игры нет → есть: программа поднимается ровно один раз, знак снят до
// запуска, сторож выходит.
func TestServeLaunchesOnce(t *testing.T) {
	held := false
	g := &fakeGuard{}
	run, polls := snapshots(false, false, true, true, true)
	launches := 0
	r := Serve(Env{
		Acquire: acquirer(&held, g),
		Wanted:  func() bool { return true },
		Running: run,
		Launch: func(fromMarker bool) error {
			if held {
				t.Fatal("знак сторожа не снят до запуска программы")
			}
			if fromMarker {
				t.Fatal("метки не было")
			}
			launches++
			return nil
		},
	})
	if r != Launched || launches != 1 {
		t.Fatalf("итог %v, запусков %d", r, launches)
	}
	if *polls != 3 {
		t.Fatalf("снимков %d, ждал 3 (нет, нет, есть)", *polls)
	}
}

// Игра шла, когда закрыли программу: это не запуск. Запуск — только после
// закрытия и нового появления.
func TestServeGameAlreadyRunning(t *testing.T) {
	held := false
	g := &fakeGuard{}
	run, polls := snapshots(true, true, false, false, true)
	launches := 0
	Serve(Env{
		Acquire: acquirer(&held, g),
		Wanted:  func() bool { return true },
		Running: run,
		Launch:  func(bool) error { launches++; return nil },
	})
	if launches != 1 || *polls != 5 {
		t.Fatalf("запусков %d, снимков %d", launches, *polls)
	}
}

// Ошибка снимка — снимок пропускается, сторож не падает.
func TestServeSnapshotErrors(t *testing.T) {
	held := false
	g := &fakeGuard{}
	n := 0
	r := Serve(Env{
		Acquire: acquirer(&held, g),
		Wanted:  func() bool { return true },
		Running: func() (bool, error) {
			n++
			switch n {
			case 1:
				return false, nil
			case 2, 3:
				return false, errors.New("нет доступа")
			}
			return true, nil
		},
		Launch: func(bool) error { return nil },
	})
	if r != Launched {
		t.Fatalf("итог %v", r)
	}
}

// Одна копия: пока первый сторож держит знак, второй сразу выходит.
func TestServeSingleCopy(t *testing.T) {
	held := true // знак держит другой сторож
	launches := 0
	r := Serve(Env{
		Acquire: acquirer(&held, &fakeGuard{}),
		Wanted:  func() bool { return true },
		Running: func() (bool, error) {
			t.Fatal("второй сторож снимает процессы")
			return false, nil
		},
		Launch: func(bool) error { launches++; return nil },
	})
	if r != Busy || launches != 0 || !held {
		t.Fatalf("итог %v, запусков %d, знак %v", r, launches, held)
	}
}

// Программа запустилась сама и попросила сторожа закрыться.
func TestServeStopped(t *testing.T) {
	held := false
	g := &fakeGuard{}
	g.stop = func() bool { return g.waits >= 2 }
	r := Serve(Env{
		Acquire: acquirer(&held, g),
		Wanted:  func() bool { return true },
		Running: func() (bool, error) { return false, nil },
		Launch:  func(bool) error { t.Fatal("запуск после просьбы закрыться"); return nil },
	})
	if r != Stopped || held || !g.released {
		t.Fatalf("итог %v, знак %v", r, held)
	}
}

// Всё «вместе с игрой» выключено — сторож сразу выходит.
func TestServeNotWanted(t *testing.T) {
	held := false
	r := Serve(Env{
		Acquire: acquirer(&held, &fakeGuard{}),
		Wanted:  func() bool { return false },
		Running: func() (bool, error) { t.Fatal("снимки без нужды"); return false, nil },
		Launch:  func(bool) error { t.Fatal("запуск без нужды"); return nil },
	})
	if r != NotWanted || held {
		t.Fatalf("итог %v, знак %v", r, held)
	}
}

// Подняла задача по метке — программа сразу, без снимков.
func TestServeMarker(t *testing.T) {
	held := false
	var from bool
	r := Serve(Env{
		Acquire: acquirer(&held, &fakeGuard{}),
		Wanted:  func() bool { return false },
		Marker:  func() bool { return true },
		Running: func() (bool, error) { t.Fatal("снимки при метке"); return false, nil },
		Launch:  func(m bool) error { from = m; return nil },
	})
	if r != Launched || !from {
		t.Fatalf("итог %v, метка %v", r, from)
	}
	r = Serve(Env{
		Acquire: acquirer(&held, &fakeGuard{}),
		Marker:  func() bool { return true },
		Launch:  func(bool) error { return errors.New("нет") },
	})
	if r != Failed {
		t.Fatalf("итог %v", r)
	}
}

func TestHowToLaunch(t *testing.T) {
	cases := []struct {
		admin, marker, task bool
		want                Way
	}{
		{true, false, true, Direct},
		{true, true, true, Direct},
		{true, false, false, Direct},
		{false, false, true, ViaTask},
		{false, true, true, Ask}, // задача уже не дала прав — по кругу не зовём
		{false, false, false, Ask},
	}
	for _, c := range cases {
		if got := HowToLaunch(c.admin, c.marker, c.task); got != c.want {
			t.Errorf("HowToLaunch(%v, %v, %v) = %v, ждал %v", c.admin, c.marker, c.task, got, c.want)
		}
	}
	if a := LaunchArgs(); len(a) != 2 || a[0] != "-autostart" || a[1] != "-from-watch" {
		t.Fatal(a)
	}
}

func TestDecisions(t *testing.T) {
	type S = settings.Settings
	cases := []struct {
		s                     S
		wanted                bool
		mode                  autostart.Mode
		exitPlain, exitUpdate bool
	}{
		{S{}, false, autostart.Off, false, false},
		{S{QuitWithGame: true}, false, autostart.Off, false, false},
		{S{ShowWithGame: true}, true, autostart.Watch, true, false},
		{S{StartWithGame: true, QuitWithGame: true}, true, autostart.Watch, true, false},
		{S{StartWithWindows: true}, false, autostart.App, false, false},
		{S{StartWithWindows: true, ShowWithGame: true}, true, autostart.App, true, false},
	}
	for i, c := range cases {
		if Wanted(c.s) != c.wanted || TaskMode(c.s) != c.mode {
			t.Errorf("%d: Wanted %v, TaskMode %v", i, Wanted(c.s), TaskMode(c.s))
		}
		if OnExit(c.s, false, false) != c.exitPlain || OnExit(c.s, false, true) != c.exitUpdate {
			t.Errorf("%d: OnExit", i)
		}
		if OnExit(c.s, true, false) {
			t.Errorf("%d: сторож при выключении Windows", i)
		}
	}
}

func TestMarker(t *testing.T) {
	dir := t.TempDir()
	now := time.Unix(1_800_000_000, 0)
	if TakeMarker(dir, now) {
		t.Fatal("метки нет")
	}
	if err := WriteMarker(dir, now); err != nil {
		t.Fatal(err)
	}
	if !TakeMarker(dir, now.Add(30*time.Second)) {
		t.Fatal("свежая метка")
	}
	if TakeMarker(dir, now) {
		t.Fatal("метка забрана дважды")
	}
	WriteMarker(dir, now)
	if TakeMarker(dir, now.Add(MarkerTTL+time.Second)) {
		t.Fatal("старая метка")
	}
	if _, err := os.Stat(filepath.Join(dir, MarkerFile)); !os.IsNotExist(err) {
		t.Fatal("старая метка не удалена")
	}
	os.WriteFile(filepath.Join(dir, MarkerFile), []byte("мусор"), 0644)
	if TakeMarker(dir, now) {
		t.Fatal("мусор")
	}
}
