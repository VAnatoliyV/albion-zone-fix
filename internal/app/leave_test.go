package app

import (
	"testing"
	"time"

	"albionzonefix/internal/avalon"
	"albionzonefix/internal/game"
	"albionzonefix/internal/photon"
	"albionzonefix/internal/zonecard"
)

// Серверы Европы (для карты), как в живой игре.
const (
	srvA = "193.169.238.10:5056"
	srvB = "193.169.238.20:5056"
	srvC = "193.169.238.30:5056"
)

// connect — Photon CONNECT от клиента к игровому серверу.
func connect() []byte {
	b := make([]byte, 24)
	b[0], b[1], b[3], b[12] = 0xff, 0xff, 1, 2
	return b
}

func ccAt(a *App, t time.Time, addr string) {
	a.Feed(game.Packet{T: t, Out: true, Addr: addr, Payload: change()})
}

func connectAt(a *App, t time.Time, addr string) {
	a.Feed(game.Packet{T: t, Out: true, Addr: addr, Payload: connect()})
}

func newMapApp(t *testing.T) (*App, *fakeMap, *[]string) {
	dir := t.TempDir()
	a := New(dir, dir, nil)
	m := &fakeMap{}
	var zones []string
	a.AttachMap(m, func(c string) { zones = append(zones, c) })
	return a, m, &zones
}

// Как у тестера 8 октября: Брецилиен → Туманы (входа с локацией нет) →
// дорога. Прохода «5001 → TNL-114» быть не должно, а тултип в Туманах не
// должен уйти с from=5001.
func TestMistsWithoutJoinIsNotAPass(t *testing.T) {
	a, m, zones := newMapApp(t)
	t0 := time.Unix(5000, 0)
	joinAt(a, t0, srvA, "5001")
	// Ушёл в Туманы: ChangeCluster, CONNECT к новому серверу, Join нет.
	ccAt(a, t0.Add(10*time.Second), srvA)
	connectAt(a, t0.Add(10500*time.Millisecond), srvB)
	replyAt(a, t0.Add(11*time.Second), srvB)
	// Пока < 3 с — место прежнее (обычный переход ещё грузится).
	a.Tick(t0.Add(12 * time.Second))
	if a.HereCode() != "5001" {
		t.Fatalf("рано сбросили место: %q", a.HereCode())
	}
	// Дольше 3 с без Join — место неизвестно.
	a.Tick(t0.Add(14 * time.Second))
	if a.HereCode() != "" {
		t.Fatalf("в Туманах без Join место должно быть неизвестно: %q", a.HereCode())
	}
	if st := a.State(); st.Here == nil || st.Here.Code != "" || st.Here.Known || !st.Here.Since.Equal(t0.Add(10*time.Second)) {
		t.Fatalf("вкладка «Зона»: %+v", st.Here)
	}
	if len(*zones) != 2 || (*zones)[1] != "" {
		t.Fatalf("подсветка: %v", *zones)
	}
	// Тултип в Туманах: откуда — неизвестно.
	if d := a.SetCard(shotFor(t, portal164, t0.Add(60*time.Second)), zonecard.Default()); d != "не отправлено (noPlace)" {
		t.Fatal(d)
	}
	// Из Туманов на дорогу: второй уход и вход в TNL-114.
	ccAt(a, t0.Add(5*time.Minute), srvB)
	connectAt(a, t0.Add(5*time.Minute+time.Second), srvC)
	joinAt(a, t0.Add(5*time.Minute+3*time.Second), srvC, "TNL-114")
	if len(m.offered) != 0 || len(m.noted) != 0 || len(m.tips) != 0 {
		t.Fatalf("проход через Туманы склеен: %+v %v %+v", m.offered, m.noted, m.tips)
	}
	if a.HereCode() != "TNL-114" {
		t.Fatalf("после Туманов: %q", a.HereCode())
	}
	// Дальше по дорогам — обычные проходы.
	ccAt(a, t0.Add(10*time.Minute), srvC)
	connectAt(a, t0.Add(10*time.Minute+time.Second), srvA)
	joinAt(a, t0.Add(10*time.Minute+2*time.Second), srvA, "TNL-001")
	if len(m.offered) != 1 || m.offered[0] != (avalon.Pass{From: "TNL-114", To: "TNL-001", Region: "europe"}) {
		t.Fatalf("проход после Туманов: %+v", m.offered)
	}
}

// Обычный переход дольше 3 с: место на время загрузки неизвестно, но
// проход не теряется.
func TestSlowNormalTransitionKeepsPass(t *testing.T) {
	a, m, zones := newMapApp(t)
	t0 := time.Unix(5000, 0)
	joinAt(a, t0, srvA, "TNL-001")
	ccAt(a, t0.Add(10*time.Second), srvA)
	ccAt(a, t0.Add(11*time.Second), srvA) // повтор того же ухода
	connectAt(a, t0.Add(11500*time.Millisecond), srvB)
	connectAt(a, t0.Add(12*time.Second), srvB) // повтор CONNECT
	ccAt(a, t0.Add(12500*time.Millisecond), srvA)
	a.CheckStall(t0.Add(13250 * time.Millisecond))
	if a.HereCode() != "" {
		t.Fatalf("3 с без Join — место неизвестно: %q", a.HereCode())
	}
	joinAt(a, t0.Add(14500*time.Millisecond), srvB, "TNL-002")
	if len(m.offered) != 1 || m.offered[0] != (avalon.Pass{From: "TNL-001", To: "TNL-002", Region: "europe"}) {
		t.Fatalf("проход потерян: %+v", m.offered)
	}
	if a.HereCode() != "TNL-002" || a.State().Here.Code != "TNL-002" {
		t.Fatalf("после входа: %q", a.HereCode())
	}
	if len(*zones) != 3 || (*zones)[1] != "" || (*zones)[2] != "TNL-002" {
		t.Fatalf("подсветка: %v", *zones)
	}
}

// Короткие переходы: 5001 → TNL (если бы портал был) и TNL → TNL на том же
// сервере без CONNECT — проходы; карточка до 3 с знает прежнее место.
func TestQuickTransitionsArePasses(t *testing.T) {
	a, m, _ := newMapApp(t)
	t0 := time.Unix(5000, 0)
	joinAt(a, t0, srvA, "5001")
	ccAt(a, t0.Add(10*time.Second), srvA)
	connectAt(a, t0.Add(10500*time.Millisecond), srvB)
	if d := a.SetCard(shotFor(t, portal164, t0.Add(11*time.Second)), zonecard.Default()); d != "портал 5001 → TNL-164 отправляю" {
		t.Fatal(d)
	}
	joinAt(a, t0.Add(12*time.Second), srvB, "TNL-114")
	ccAt(a, t0.Add(60*time.Second), srvB)
	joinAt(a, t0.Add(62*time.Second), srvB, "TNL-001")
	if len(m.offered) != 2 || m.offered[0].From != "5001" || m.offered[0].To != "TNL-114" ||
		m.offered[1].From != "TNL-114" || m.offered[1].To != "TNL-001" {
		t.Fatalf("проходы: %+v", m.offered)
	}
}

func replyAt(a *App, t time.Time, addr string) {
	a.Feed(game.Packet{T: t, Addr: addr, Payload: pkt(photon.MsgEvent, 1, []byte{0})})
}

// Переход без ChangeCluster (по CONNECT): побывали на новом сервере (он
// ответил) и без Join ушли на следующий — не проход. Новый сервер молчал
// и клиент сменил цель — тот же переход, проход. ChangeCluster без CONNECT
// и Join забывается, а подтверждённый Join — один уход.
func TestTwoLeavesWithoutChangeClusterOrConnect(t *testing.T) {
	a, m, _ := newMapApp(t)
	t0 := time.Unix(5000, 0)
	joinAt(a, t0, srvA, "TNL-001")
	connectAt(a, t0.Add(10*time.Second), srvB)
	replyAt(a, t0.Add(10500*time.Millisecond), srvB)
	connectAt(a, t0.Add(70*time.Second), srvC)
	joinAt(a, t0.Add(72*time.Second), srvC, "TNL-002")
	if len(m.offered) != 0 {
		t.Fatalf("два ухода — не проход: %+v", m.offered)
	}
	// Смена цели: B молчит, клиент идёт на A.
	ccAt(a, t0.Add(100*time.Second), srvC)
	connectAt(a, t0.Add(101*time.Second), srvB)
	connectAt(a, t0.Add(105*time.Second), srvA)
	joinAt(a, t0.Add(107*time.Second), srvA, "TNL-003")
	if len(m.offered) != 1 || m.offered[0].From != "TNL-002" || m.offered[0].To != "TNL-003" {
		t.Fatalf("смена цели — тот же переход: %+v", m.offered)
	}
	// Отменённый ChangeCluster, потом переход на том же сервере.
	ccAt(a, t0.Add(200*time.Second), srvA)
	ccAt(a, t0.Add(300*time.Second), srvA)
	joinAt(a, t0.Add(302*time.Second), srvA, "TNL-004")
	if len(m.offered) != 2 || m.offered[1].From != "TNL-003" {
		t.Fatalf("проход: %+v", m.offered)
	}
	// Переподключение к тому же серверу — не уход.
	connectAt(a, t0.Add(400*time.Second), srvA)
	ccAt(a, t0.Add(500*time.Second), srvA)
	joinAt(a, t0.Add(502*time.Second), srvA, "TNL-005")
	if len(m.offered) != 3 || m.offered[2].From != "TNL-004" {
		t.Fatalf("проход: %+v", m.offered)
	}
}

func TestLeaveCounter(t *testing.T) {
	t0 := time.Unix(1000, 0)
	at := func(s float64) time.Time { return t0.Add(time.Duration(s * float64(time.Second))) }
	ev := func(s float64, k game.Kind, srv string) game.Ev { return game.Ev{T: at(s), Kind: k, Server: srv} }
	cases := []struct {
		name string
		evs  []game.Ev
		join float64
		want int
	}{
		{"обычный", []game.Ev{ev(1, game.ChangeCluster, "a"), ev(1.5, game.Connect, "b")}, 3, 1},
		{"без CONNECT", []game.Ev{ev(1, game.ChangeCluster, "a")}, 3, 1},
		{"отменённый ChangeCluster", []game.Ev{ev(1, game.ChangeCluster, "a")}, 100, 0},
		{"повторы", []game.Ev{ev(1, game.ChangeCluster, "a"), ev(2, game.ChangeCluster, "a"), ev(2.5, game.Connect, "b"),
			ev(3, game.Connect, "b"), ev(3.5, game.ChangeCluster, "a"), ev(20, game.Connect, "b")}, 22, 1},
		{"долгие повторы", []game.Ev{ev(1, game.ChangeCluster, "a"), ev(2, game.Connect, "b"), ev(20, game.ChangeCluster, "a"),
			ev(40, game.ChangeCluster, "a"), ev(60, game.ChangeCluster, "a")}, 62, 1},
		{"Туманы на новом сервере", []game.Ev{ev(1, game.ChangeCluster, "a"), ev(1.5, game.Connect, "b"), ev(2, game.Reply, "b"),
			ev(90, game.ChangeCluster, "b"), ev(91, game.Connect, "c")}, 93, 2},
		{"Туманы, выход на том же сервере", []game.Ev{ev(1, game.ChangeCluster, "a"), ev(1.5, game.Connect, "b"),
			ev(90, game.ChangeCluster, "b")}, 92, 2},
		{"два новых сервера", []game.Ev{ev(1, game.Connect, "b"), ev(1.2, game.Reply, "b"), ev(2, game.Connect, "c")}, 3, 2},
		{"смена цели", []game.Ev{ev(1, game.Connect, "b"), ev(2, game.Connect, "c")}, 3, 1},
		{"переподключение", []game.Ev{ev(1, game.Connect, "a")}, 3, 0},
	}
	for _, c := range cases {
		l := leaveCounter{}
		l.on(game.Ev{T: t0, Kind: game.Join, Server: "a"})
		for _, e := range c.evs {
			l.on(e)
		}
		if got := l.on(game.Ev{T: at(c.join), Kind: game.Join, Server: "z"}); got != c.want {
			t.Errorf("%s: уходов %d, ждали %d", c.name, got, c.want)
		}
		if l.n != 0 || l.away(at(1000)) {
			t.Errorf("%s: Join не обнулил счёт", c.name)
		}
	}
	// Место неизвестно через awayAfter после первого подтверждённого ухода;
	// неподтверждённый ChangeCluster место не трогает.
	l := leaveCounter{}
	l.on(game.Ev{T: t0, Kind: game.Join, Server: "a"})
	l.on(ev(1, game.ChangeCluster, "a"))
	if l.away(at(5)) {
		t.Fatal("ChangeCluster без подтверждения")
	}
	l.on(ev(2, game.Connect, "b"))
	if l.away(at(3.9)) || !l.away(at(4)) {
		t.Fatal("awayAfter")
	}
}

// ChangeCluster без CONNECT и Join (отменён, отказ) — не уход: место не
// становится неизвестным, следующий настоящий переход — проход.
func TestRefusedChangeClusterIsForgotten(t *testing.T) {
	a, m, _ := newMapApp(t)
	t0 := time.Unix(5000, 0)
	joinAt(a, t0, srvA, "TNL-001")
	ccAt(a, t0.Add(10*time.Second), srvA)
	for s := 11; s <= 40; s++ {
		a.Tick(t0.Add(time.Duration(s) * time.Second))
	}
	if a.HereCode() != "TNL-001" {
		t.Fatalf("отменённый уход сбросил место: %q", a.HereCode())
	}
	if d := a.SetCard(shotFor(t, portal164, t0.Add(40*time.Second)), zonecard.Default()); d != "портал TNL-001 → TNL-164 отправляю" {
		t.Fatal(d)
	}
	ccAt(a, t0.Add(60*time.Second), srvA)
	connectAt(a, t0.Add(61*time.Second), srvB)
	joinAt(a, t0.Add(63*time.Second), srvB, "TNL-002")
	if len(m.offered) != 1 || m.offered[0] != (avalon.Pass{From: "TNL-001", To: "TNL-002", Region: "europe"}) {
		t.Fatalf("проход после отменённого ухода потерян: %+v", m.offered)
	}
}
