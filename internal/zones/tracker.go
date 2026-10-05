// Пакет zones ведёт учёт переходов между локациями и считает, какие из них
// грузятся дольше всех и где игра вылетает.
package zones

import (
	"math"
	"sort"
	"time"

	"albionzonefix/internal/game"
)

const (
	failAfter  = 30 * time.Second // нет входа в новую локацию — считаем вылетом
	aliveAfter = 15 * time.Second // сколько ждём первого события с нового сервера
)

// Transition — один переход. LoadSec: от запроса смены кластера до входа в
// новую локацию. AliveSec: от входа до первого события с нового сервера,
// то есть сколько ещё стоял «пустой» экран (−1 — событий не дождались).
type Transition struct {
	T        time.Time `json:"t"`
	From     string    `json:"from"`
	To       string    `json:"to"`
	FromName string    `json:"fromName"`
	ToName   string    `json:"toName"`
	Server   string    `json:"server"`
	LoadSec  float64   `json:"loadSec"`
	AliveSec float64   `json:"aliveSec"`
	OK       bool      `json:"ok"`
	Fail     string    `json:"fail,omitempty"`
	Strategy string    `json:"strategy"`
}

type Tracker struct {
	name     func(string) string
	strategy func() string
	done     func(Transition)

	cur     string    // где сейчас
	start   time.Time // когда попросили смену кластера (нулевое — не ждём)
	from    string
	waiting *Transition // вошли, ждём первого события
	joinT   time.Time
}

func NewTracker(name func(string) string, strategy func() string, done func(Transition)) *Tracker {
	return &Tracker{name: name, strategy: strategy, done: done}
}

func round1(x float64) float64 { return math.Round(x*10) / 10 }

func (t *Tracker) On(e game.Ev) {
	switch e.Kind {
	case game.ChangeCluster:
		t.flushWaiting(-1)
		if t.start.IsZero() {
			t.start, t.from = e.T, t.cur
		}
	case game.Join:
		t.flushWaiting(-1)
		if !t.start.IsZero() {
			tr := Transition{T: e.T, From: t.from, To: e.Location, FromName: t.name(t.from), ToName: t.name(e.Location),
				Server: e.Server, LoadSec: round1(e.T.Sub(t.start).Seconds()), AliveSec: -1, OK: true, Strategy: t.strategy()}
			t.waiting, t.joinT = &tr, e.T
			t.start = time.Time{}
		}
		t.cur = e.Location
	case game.Incoming:
		if t.waiting != nil && e.Server == t.waiting.Server && e.T.After(t.joinT) {
			t.flushWaiting(round1(e.T.Sub(t.joinT).Seconds()))
		}
	}
}

func (t *Tracker) flushWaiting(alive float64) {
	if t.waiting == nil {
		return
	}
	tr := *t.waiting
	tr.AliveSec = alive
	t.waiting = nil
	t.done(tr)
}

// Tick закрывает зависшие ожидания. Вызывать раз в секунду.
func (t *Tracker) Tick(now time.Time) {
	if t.waiting != nil && now.Sub(t.joinT) > aliveAfter {
		t.flushWaiting(-1)
	}
	if !t.start.IsZero() && now.Sub(t.start) > failAfter {
		t.done(Transition{T: now, From: t.from, FromName: t.name(t.from), LoadSec: round1(now.Sub(t.start).Seconds()),
			AliveSec: -1, OK: false, Fail: "нет входа в новую локацию 30 с", Strategy: t.strategy()})
		t.start, t.cur = time.Time{}, "" // после вылета следующий вход — вход в игру
	}
}

// ZoneStat — сводка по локации назначения.
type ZoneStat struct {
	Zone     string  `json:"zone"`
	Name     string  `json:"name"`
	Count    int     `json:"count"`
	Fails    int     `json:"fails"`
	AvgLoad  float64 `json:"avgLoad"`
	MaxLoad  float64 `json:"maxLoad"`
	AvgAlive float64 `json:"avgAlive"`
}

// Worst — худшие локации: сначала по вылетам, потом по среднему времени.
// withBypass выбирает переходы с включённым обходом (strategy != "off").
// Вылеты без известной цели собираются под пустым кодом «?».
func Worst(trs []Transition, withBypass bool) []ZoneStat {
	type acc struct {
		s                 ZoneStat
		loadSum, aliveSum float64
		loadN, aliveN     int
	}
	m := map[string]*acc{}
	for _, tr := range trs {
		if (tr.Strategy != "off") != withBypass {
			continue
		}
		key, name := tr.To, tr.ToName
		if key == "" {
			key, name = "?", "вылет до входа, из "+tr.FromName
		}
		a := m[key]
		if a == nil {
			a = &acc{s: ZoneStat{Zone: key, Name: name}}
			m[key] = a
		}
		a.s.Count++
		if !tr.OK {
			a.s.Fails++
			continue
		}
		a.loadSum += tr.LoadSec
		a.loadN++
		if tr.LoadSec > a.s.MaxLoad {
			a.s.MaxLoad = tr.LoadSec
		}
		if tr.AliveSec >= 0 {
			a.aliveSum += tr.AliveSec
			a.aliveN++
		}
	}
	out := make([]ZoneStat, 0, len(m))
	for _, a := range m {
		if a.loadN > 0 {
			a.s.AvgLoad = round1(a.loadSum / float64(a.loadN))
		}
		if a.aliveN > 0 {
			a.s.AvgAlive = round1(a.aliveSum / float64(a.aliveN))
		}
		out = append(out, a.s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Fails != out[j].Fails {
			return out[i].Fails > out[j].Fails
		}
		return out[i].AvgLoad+out[i].AvgAlive > out[j].AvgLoad+out[j].AvgAlive
	})
	return out
}
