package zones

import (
	"testing"
	"time"

	"albionzonefix/internal/game"
)

var t0 = time.Unix(10000, 0)

func at(sec float64) time.Time { return t0.Add(time.Duration(sec * float64(time.Second))) }

func newT(done *[]Transition) *Tracker {
	names := map[string]string{"0000": "Thetford", "4002": "Swamp Road"}
	return NewTracker(func(c string) string { return names[c] }, func() string { return "off" },
		func(tr Transition) { *done = append(*done, tr) })
}

func TestLoginIsNotATransition(t *testing.T) {
	var done []Transition
	tr := newT(&done)
	tr.On(game.Ev{T: at(0), Kind: game.Join, Server: "a:5056", Location: "0000"})
	tr.Tick(at(60))
	if len(done) != 0 {
		t.Fatalf("вход в игру записан как переход: %+v", done)
	}
}

func TestNormalTransitionWithAliveTime(t *testing.T) {
	var done []Transition
	tr := newT(&done)
	tr.On(game.Ev{T: at(0), Kind: game.Join, Server: "a:5056", Location: "0000"})
	tr.On(game.Ev{T: at(100), Kind: game.ChangeCluster, Server: "a:5056"})
	tr.On(game.Ev{T: at(103.5), Kind: game.Join, Server: "b:5056", Location: "4002"})
	tr.On(game.Ev{T: at(103.6), Kind: game.Incoming, Server: "a:5056"}) // со старого сервера не считается
	tr.On(game.Ev{T: at(109.5), Kind: game.Incoming, Server: "b:5056"})
	if len(done) != 1 {
		t.Fatalf("переходов %d", len(done))
	}
	g := done[0]
	if g.From != "0000" || g.To != "4002" || g.FromName != "Thetford" || g.ToName != "Swamp Road" ||
		g.LoadSec != 3.5 || g.AliveSec != 6 || !g.OK || g.Strategy != "off" || g.Server != "b:5056" {
		t.Fatalf("переход: %+v", g)
	}
}

func TestStuckTransitionIsAFailure(t *testing.T) {
	var done []Transition
	tr := newT(&done)
	tr.On(game.Ev{T: at(0), Kind: game.Join, Server: "a:5056", Location: "0000"})
	tr.On(game.Ev{T: at(10), Kind: game.ChangeCluster, Server: "a:5056"})
	tr.Tick(at(30))
	if len(done) != 0 {
		t.Fatal("рано записали вылет")
	}
	tr.Tick(at(41))
	if len(done) != 1 || done[0].OK || done[0].Fail == "" || done[0].From != "0000" {
		t.Fatalf("ждали вылет: %+v", done)
	}
	// после вылета следующий вход — снова вход в игру, а не переход
	tr.On(game.Ev{T: at(90), Kind: game.Join, Server: "c:5056", Location: "4002"})
	tr.Tick(at(200))
	if len(done) != 1 {
		t.Fatalf("лишняя запись: %+v", done)
	}
}

func TestNoEventsAfterJoinStillRecorded(t *testing.T) {
	var done []Transition
	tr := newT(&done)
	tr.On(game.Ev{T: at(0), Kind: game.Join, Server: "a:5056", Location: "0000"})
	tr.On(game.Ev{T: at(1), Kind: game.ChangeCluster, Server: "a:5056"})
	tr.On(game.Ev{T: at(2), Kind: game.Join, Server: "b:5056", Location: "4002"})
	tr.Tick(at(30))
	if len(done) != 1 || done[0].AliveSec != -1 || !done[0].OK {
		t.Fatalf("ждали запись без событий: %+v", done)
	}
}

func TestWorstZones(t *testing.T) {
	trs := []Transition{
		{To: "4002", ToName: "Swamp Road", LoadSec: 10, OK: true, Strategy: "off"},
		{To: "4002", ToName: "Swamp Road", OK: false, Fail: "x", Strategy: "off"},
		{To: "0000", ToName: "Thetford", LoadSec: 2, OK: true, Strategy: "off"},
		{To: "4002", ToName: "Swamp Road", LoadSec: 3, OK: true, Strategy: "s1"},
	}
	off := Worst(trs, false)
	if len(off) != 2 || off[0].Zone != "4002" || off[0].Count != 2 || off[0].Fails != 1 || off[0].AvgLoad != 10 {
		t.Fatalf("без обхода: %+v", off)
	}
	on := Worst(trs, true)
	if len(on) != 1 || on[0].AvgLoad != 3 || on[0].Fails != 0 {
		t.Fatalf("с обходом: %+v", on)
	}
}
