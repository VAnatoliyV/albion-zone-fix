package gamewatch

import (
	"context"
	"sync"
	"testing"
	"time"
)

func steps(w *Watcher, seq ...bool) []Event {
	var out []Event
	for _, p := range seq {
		out = append(out, w.Step(p))
	}
	return out
}

func eq(a, b []Event) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Программа стартовала с Windows раньше игры: игры нет → есть → нет.
func TestNoneThenGameThenNone(t *testing.T) {
	var w Watcher
	got := steps(&w, false, false, true, true, false, false, false)
	want := []Event{None, None, Started, None, None, Exited, None}
	if !eq(got, want) {
		t.Fatalf("%v, ждал %v", got, want)
	}
}

// Игра шла до программы: это не запуск, а закрытие — закрытие.
func TestGameAlreadyRunning(t *testing.T) {
	var w Watcher
	got := steps(&w, true, true, false, false)
	want := []Event{None, None, None, Exited}
	if !eq(got, want) {
		t.Fatalf("%v, ждал %v", got, want)
	}
}

// Один промах снимка — не закрытие; игра снова на месте — не новый запуск.
func TestSingleMissIsNotExit(t *testing.T) {
	var w Watcher
	got := steps(&w, false, true, false, true, false, true)
	want := []Event{None, Started, None, None, None, None}
	if !eq(got, want) {
		t.Fatalf("%v, ждал %v", got, want)
	}
}

// Перезапуск игры: закрылась и запустилась снова — два события.
func TestRestart(t *testing.T) {
	var w Watcher
	got := steps(&w, false, true, false, false, true)
	want := []Event{None, Started, None, Exited, Started}
	if !eq(got, want) {
		t.Fatalf("%v, ждал %v", got, want)
	}
}

func TestResetMakesNextSnapshotBaseline(t *testing.T) {
	var w Watcher
	steps(&w, false)
	w.Reset()
	if e := w.Step(true); e != None {
		t.Fatalf("после сброса первый снимок — точка отсчёта, а не %v", e)
	}
}

func TestIsGame(t *testing.T) {
	for exe, want := range map[string]bool{
		"Albion-Online.exe": true, "albion-online.EXE": true,
		"AlbionLauncher.exe": false, "Albion-Online": false, "AlbionJournal.exe": false, "": false,
	} {
		if IsGame(exe) != want {
			t.Errorf("IsGame(%q) != %v", exe, want)
		}
	}
}

func TestDecide(t *testing.T) {
	all := Options{Show: true, Collect: true, Quit: true}
	cases := []struct {
		e          Event
		o          Options
		collecting bool
		want       Actions
	}{
		{Started, all, false, Actions{Show: true, Collect: true}},
		{Started, all, true, Actions{Show: true}}, // сбор уже идёт
		{Started, Options{Quit: true}, false, Actions{}},
		{Exited, all, true, Actions{Quit: true}},
		{Exited, Options{Show: true, Collect: true}, false, Actions{}},
		{None, all, false, Actions{}},
	}
	for _, c := range cases {
		if got := Decide(c.e, c.o, c.collecting); got != c.want {
			t.Errorf("Decide(%v, %+v, %v) = %+v, ждал %+v", c.e, c.o, c.collecting, got, c.want)
		}
	}
}

// Run: снимки по очереди, события — в On; выключено всё — снимков нет.
func TestRunFeedsWatcher(t *testing.T) {
	seq := []bool{false, true, true, false, false}
	var mu sync.Mutex
	i, polls := 0, 0
	on := Options{Quit: true}
	var events []Event
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		Run(ctx, Config{
			Every:   time.Millisecond,
			Options: func() Options { mu.Lock(); defer mu.Unlock(); return on },
			Running: func() (bool, error) {
				mu.Lock()
				defer mu.Unlock()
				polls++
				if i >= len(seq) {
					return false, ErrUnsupported // ошибка — снимок пропускается
				}
				i++
				return seq[i-1], nil
			},
			On: func(e Event) { mu.Lock(); events = append(events, e); mu.Unlock() },
		})
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(events)
		mu.Unlock()
		if n == 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	mu.Lock()
	got := append([]Event(nil), events...)
	on = Options{}
	before := polls
	mu.Unlock()
	if !eq(got, []Event{Started, Exited}) {
		t.Fatalf("события %v", got)
	}
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	after := polls
	mu.Unlock()
	if after-before > 1 { // мог успеть один снимок, начатый до выключения
		t.Fatalf("всё выключено, а снимки идут: %d", after-before)
	}
	cancel()
	<-done
}

// Программу поднял сторож: игра уже идёт, но первый снимок — запуск.
func TestRunJustStarted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan Event, 4)
	go Run(ctx, Config{
		Every:       time.Millisecond,
		JustStarted: true,
		Options:     func() Options { return Options{Show: true} },
		Running:     func() (bool, error) { return true, nil },
		On:          func(e Event) { got <- e },
	})
	select {
	case e := <-got:
		if e != Started {
			t.Fatalf("первое событие %v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("запуск не замечен")
	}
	select {
	case e := <-got:
		t.Fatalf("лишнее событие %v", e)
	case <-time.After(20 * time.Millisecond):
	}
}
