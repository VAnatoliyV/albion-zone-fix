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
	lastD    time.Duration
	released bool
}

func (g *fakeGuard) Wait(d time.Duration) bool {
	g.waits++
	g.lastD = d
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
		Launch: func() error {
			// Знак держится до выхода процесса: пока сторож поднимает
			// программу, StopRunning его видит.
			if !held {
				t.Fatal("знак сторожа снят до запуска программы")
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
		Launch:  func() error { launches++; return nil },
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
		Launch: func() error { return nil },
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
		Launch: func() error { launches++; return nil },
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
		Launch:  func() error { t.Fatal("запуск после просьбы закрыться"); return nil },
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
		Launch:  func() error { t.Fatal("запуск без нужды"); return nil },
	})
	if r != NotWanted || held {
		t.Fatalf("итог %v, знак %v", r, held)
	}
}

func TestHowToLaunch(t *testing.T) {
	cases := []struct {
		admin bool
		task  autostart.Mode
		want  Way
	}{
		{true, autostart.App, Direct},
		{true, autostart.Watch, Direct},
		{true, autostart.Off, Direct},
		{false, autostart.App, ViaTask},
		// Задача — сам этот сторож: /Run при IgnoreNew пропадёт молча.
		{false, autostart.Watch, Ask},
		{false, autostart.Off, Ask},
	}
	for _, c := range cases {
		if got := HowToLaunch(c.admin, c.task); got != c.want {
			t.Errorf("HowToLaunch(%v, %v) = %v, ждал %v", c.admin, c.task, got, c.want)
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
		if OnExit(c.s, false, false, false) != c.exitPlain || OnExit(c.s, false, true, false) != c.exitUpdate {
			t.Errorf("%d: OnExit", i)
		}
		if OnExit(c.s, true, false, false) {
			t.Errorf("%d: сторож при выключении Windows", i)
		}
		if OnExit(c.s, false, false, true) {
			t.Errorf("%d: сторож после «Выйти совсем»", i)
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

// Пока сторож ждал, программа запустилась (просьба закрыться пришла перед
// самым запуском) — программу не поднимаем.
func TestServeStopJustBeforeLaunch(t *testing.T) {
	held := false
	g := &fakeGuard{}
	g.stop = func() bool { return g.lastD == 0 } // просьба видна только проверке перед запуском
	run, _ := snapshots(false, true)
	r := Serve(Env{
		Acquire: acquirer(&held, g),
		Wanted:  func() bool { return true },
		Running: run,
		Launch:  func() error { t.Fatal("запуск после просьбы закрыться"); return nil },
	})
	if r != Stopped {
		t.Fatalf("итог %v", r)
	}
}

// Программа ещё закрывается (знак одной копии держит) — подождать её
// выхода и поднять; не закрылась — не поднимать (вторая копия показала бы
// сообщение поверх игры).
func TestServeWaitsForDyingApp(t *testing.T) {
	for _, gone := range []bool{true, false} {
		held := false
		g := &fakeGuard{}
		run, _ := snapshots(false, true)
		checks, launches := 0, 0
		r := Serve(Env{
			Acquire: acquirer(&held, g),
			Wanted:  func() bool { return true },
			Running: run,
			AppRunning: func() bool {
				checks++
				return !gone || checks < 3
			},
			AppWait: 10 * time.Millisecond,
			Launch:  func() error { launches++; return nil },
		})
		if gone && (r != Launched || launches != 1) {
			t.Fatalf("программа вышла: итог %v, запусков %d", r, launches)
		}
		if !gone && (r != AppAlive || launches != 0) {
			t.Fatalf("программа жива: итог %v, запусков %d", r, launches)
		}
	}
}

// Через задачу: метка пишется до /Run и убирается, если /Run не вышел.
func TestLaunchViaTask(t *testing.T) {
	dir := t.TempDir()
	now := time.Unix(1_800_000_000, 0)
	err := LaunchViaTask(dir, now, func() error {
		if _, err := os.Stat(filepath.Join(dir, MarkerFile)); err != nil {
			t.Fatal("метки нет к /Run")
		}
		return errors.New("отказано")
	})
	if err == nil {
		t.Fatal("ошибка /Run потерялась")
	}
	if _, err := os.Stat(filepath.Join(dir, MarkerFile)); !os.IsNotExist(err) {
		t.Fatal("метка осталась после неудачного /Run")
	}
	if err := LaunchViaTask(dir, now, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if !TakeMarker(dir, now.Add(5*time.Second)) {
		t.Fatal("метка после удачного /Run")
	}
	if MarkerTTL > 30*time.Second {
		t.Fatalf("метка живёт %v: ручной запуск позже примет её за запуск игры", MarkerTTL)
	}
}

// Задача при входе: сбой сторожа «вместе с игрой» не откатывает настройку,
// но и не теряется — ошибка уходит на страницу.
func TestSyncTask(t *testing.T) {
	type S = settings.Settings
	fail := errors.New("schtasks: отказано")
	var got []autostart.Mode
	sync := func(m autostart.Mode) error { got = append(got, m); return fail }
	if rb, err := SyncTask(S{}, S{ShowWithGame: true}, sync); rb || !errors.Is(err, fail) {
		t.Fatalf("сторож: откат %v, ошибка %v", rb, err)
	}
	if rb, err := SyncTask(S{}, S{StartWithWindows: true}, sync); !rb || !errors.Is(err, fail) {
		t.Fatalf("запуск с Windows: откат %v, ошибка %v", rb, err)
	}
	got = nil
	if rb, err := SyncTask(S{ShowWithGame: true}, S{ShowWithGame: true, QuitWithGame: true}, sync); rb || err != nil || got != nil {
		t.Fatalf("режим не менялся: откат %v, ошибка %v, вызовы %v", rb, err, got)
	}
	ok := func(m autostart.Mode) error { got = append(got, m); return nil }
	if rb, err := SyncTask(S{ShowWithGame: true}, S{}, ok); rb || err != nil || got[len(got)-1] != autostart.Off {
		t.Fatalf("снять задачу: %v %v %v", rb, err, got)
	}
}

// StopRunning: событие ещё не создано — просьба повторяется; ждём выхода
// процесса, а не только снятия знака.
func TestStopWaitsForProcess(t *testing.T) {
	signals, opens, waited := 0, 0, time.Duration(0)
	now := time.Unix(0, 0)
	was, ok := stop(stopOps{
		running: func() bool { return true },
		signal: func() bool {
			signals++
			return signals >= 3 // событие появилось с третьей попытки
		},
		open: func() (func(time.Duration) bool, bool) {
			opens++
			return func(d time.Duration) bool { waited = d; return true }, true
		},
		sleep: func(d time.Duration) { now = now.Add(d) },
		now:   func() time.Time { return now },
	}, 3*time.Second)
	if !was || !ok || signals != 3 || waited <= 0 {
		t.Fatalf("was %v ok %v, просьб %d, ждали процесс %v", was, ok, signals, waited)
	}
	// Процесс не вышел за отведённое — не «закрыт».
	was, ok = stop(stopOps{
		running: func() bool { return true },
		signal:  func() bool { return true },
		open:    func() (func(time.Duration) bool, bool) { return func(time.Duration) bool { return false }, true },
		sleep:   func(d time.Duration) { now = now.Add(d) },
		now:     func() time.Time { return now },
	}, time.Second)
	if !was || ok {
		t.Fatalf("не вышел: was %v ok %v", was, ok)
	}
	// Сторожа нет.
	if was, ok := stop(stopOps{running: func() bool { return false }}, time.Second); was || !ok {
		t.Fatal("сторожа нет")
	}
	// Знак пропал, а процесс так и не открылся — вышел сам.
	n := 0
	was, ok = stop(stopOps{
		running: func() bool { n++; return n < 3 },
		signal:  func() bool { return false },
		open:    func() (func(time.Duration) bool, bool) { return nil, false },
		sleep:   func(d time.Duration) { now = now.Add(d) },
		now:     func() time.Time { return now },
	}, time.Second)
	if !was || !ok {
		t.Fatalf("вышел сам: was %v ok %v", was, ok)
	}
}
