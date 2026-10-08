package zones

import (
	"strings"
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

// Как в записи тестера 5 октября: игра шла на сервере .102 (локация не известна,
// запись началась посреди игры), клиент 10 с стучался на новый сервер .242,
// тот ни разу не ответил, клиент сдался.
func TestConnectWithoutAnyReplyIsAFailure(t *testing.T) {
	var done []Transition
	tr := newT(&done)
	old, nw := "193.169.238.102:5056", "193.169.238.242:5056"
	tr.On(game.Ev{T: at(0), Kind: game.Incoming, Server: old})
	tr.On(game.Ev{T: at(32.6), Kind: game.Connect, Server: nw})
	tr.On(game.Ev{T: at(33.4), Kind: game.Connect, Server: nw}) // повтор
	tr.On(game.Ev{T: at(34), Kind: game.Incoming, Server: old}) // старый ещё шлёт — не мешает
	tr.On(game.Ev{T: at(43.2), Kind: game.Disconnect, Server: nw})
	if len(done) != 1 {
		t.Fatalf("переходов %d: %+v", len(done), done)
	}
	g := done[0]
	if g.OK || g.Replied || g.Server != nw || g.LoadSec != 10.6 || !strings.Contains(g.Fail, "не ответил") {
		t.Fatalf("переход: %+v", g)
	}
}

func TestConnectReplyJoinIsSuccess(t *testing.T) {
	var done []Transition
	tr := newT(&done)
	old, nw := "a:5056", "b:5056"
	tr.On(game.Ev{T: at(0), Kind: game.Join, Server: old, Location: "0000"})
	tr.On(game.Ev{T: at(10), Kind: game.Connect, Server: nw})
	tr.On(game.Ev{T: at(10.3), Kind: game.Reply, Server: nw})
	tr.On(game.Ev{T: at(12), Kind: game.Join, Server: nw, Location: "4002"})
	tr.On(game.Ev{T: at(13), Kind: game.Incoming, Server: nw})
	if len(done) != 1 || !done[0].OK || !done[0].Replied || done[0].ReplySec != 0.3 || done[0].LoadSec != 2 || done[0].To != "4002" {
		t.Fatalf("переход: %+v", done)
	}
}

func TestReplyButNoJoinIsDifferentFailure(t *testing.T) {
	var done []Transition
	tr := newT(&done)
	tr.On(game.Ev{T: at(0), Kind: game.Incoming, Server: "a:5056"})
	tr.On(game.Ev{T: at(1), Kind: game.Connect, Server: "b:5056"})
	tr.On(game.Ev{T: at(1.2), Kind: game.Reply, Server: "b:5056"})
	tr.Tick(at(40))
	if len(done) != 1 || done[0].OK || !done[0].Replied || !strings.Contains(done[0].Fail, "ответил") || strings.Contains(done[0].Fail, "не ответил") {
		t.Fatalf("переход: %+v", done)
	}
}

func TestFirstConnectAtGameStartIsNotATransition(t *testing.T) {
	var done []Transition
	tr := newT(&done)
	tr.On(game.Ev{T: at(0), Kind: game.Connect, Server: "a:5056"})
	tr.On(game.Ev{T: at(0.2), Kind: game.Reply, Server: "a:5056"})
	tr.On(game.Ev{T: at(1), Kind: game.Join, Server: "a:5056", Location: "0000"})
	tr.Tick(at(100))
	if len(done) != 0 {
		t.Fatalf("вход в игру стал переходом: %+v", done)
	}
}

// Новый сервер молчит после CONNECT — предупреждение через StallAfter, один
// раз на переход; ответил вовремя — молчим.
func TestStallWarning(t *testing.T) {
	var done []Transition
	tr := newT(&done)
	var warns []string
	tr.OnStall(2*time.Second, func(server, from string) { warns = append(warns, server+" "+from) })
	tr.On(game.Ev{T: at(0), Kind: game.Join, Server: "a:5056", Location: "0000"})
	tr.On(game.Ev{T: at(10), Kind: game.ChangeCluster, Server: "a:5056"})
	tr.On(game.Ev{T: at(10.5), Kind: game.Connect, Server: "b:5056"})
	tr.CheckStall(at(12.25))
	if len(warns) != 0 {
		t.Fatal("рано: 1.75 с после CONNECT")
	}
	tr.CheckStall(at(12.5))
	tr.CheckStall(at(13))
	tr.CheckStall(at(20))
	if len(warns) != 1 || warns[0] != "b:5056 0000" {
		t.Fatalf("ровно одно предупреждение: %v", warns)
	}
	// Клиент переподключился к тому же серверу — переход тот же, второго нет.
	tr.On(game.Ev{T: at(21), Kind: game.Connect, Server: "b:5056"})
	tr.CheckStall(at(30))
	if len(warns) != 1 {
		t.Fatalf("%v", warns)
	}
	// Следующий переход: сервер ответил за секунду — тишина.
	tr.On(game.Ev{T: at(31), Kind: game.Join, Server: "b:5056", Location: "4002"})
	tr.On(game.Ev{T: at(40), Kind: game.ChangeCluster, Server: "b:5056"})
	tr.On(game.Ev{T: at(40.2), Kind: game.Connect, Server: "c:5056"})
	tr.On(game.Ev{T: at(41.2), Kind: game.Reply, Server: "c:5056"})
	tr.CheckStall(at(50))
	if len(warns) != 1 {
		t.Fatalf("ответил — не предупреждаем: %v", warns)
	}
	// И ещё один без ответа — новое предупреждение.
	tr.On(game.Ev{T: at(42), Kind: game.Join, Server: "c:5056", Location: "0000"})
	tr.On(game.Ev{T: at(60), Kind: game.ChangeCluster, Server: "c:5056"})
	tr.On(game.Ev{T: at(60.1), Kind: game.Connect, Server: "d:5056"})
	tr.CheckStall(at(62.2))
	if len(warns) != 2 || warns[1] != "d:5056 0000" {
		t.Fatalf("%v", warns)
	}
	// Без подписки CheckStall ничего не делает.
	var d2 []Transition
	newT(&d2).CheckStall(at(100))
}
