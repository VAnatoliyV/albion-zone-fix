package app

import (
	"testing"
	"time"

	"albionzonefix/internal/avalon"
	"albionzonefix/internal/game"
	"albionzonefix/internal/zonecard"
	"albionzonefix/internal/zones"
)

func shotFor(t *testing.T, lines []string, at time.Time) zonecard.Shot {
	t.Helper()
	r, err := zonecard.Identify(zonecard.Default(), lines, at)
	if err != nil {
		t.Fatal(err)
	}
	return zonecard.Shot{Result: r}
}

var portal164 = []string{"Road of Avalon to", "Qiient-Al-Vynsis", "7/7", "Closes in 5 h 53 m"}

func TestCardSendsTooltipAndShowsStatus(t *testing.T) {
	dir := t.TempDir()
	a := New(dir, dir, nil)
	m := &fakeMap{}
	a.AttachMap(m, nil)
	a.AttachCard(func() (bool, []string, string, bool) { return false, []string{"en-US"}, "noRu", true })
	t0 := time.Unix(5000, 0)

	// Где игрок, ещё не знаем — не шлём, честно говорим почему.
	if d := a.SetCard(shotFor(t, portal164, t0), zonecard.Default()); d != "не отправлено (noPlace)" {
		t.Fatal(d)
	}
	c := a.State().Card
	if len(m.tips) != 0 || c.MapWhy != zonecard.WhyNoPlace || c.Zone == nil || c.Zone.Code != "TNL-164" || c.Size != 7 ||
		!c.ClosesAt.Equal(t0.Add(5*time.Hour+53*time.Minute)) || c.OCRHint != "noRu" || !c.OCRChecked {
		t.Fatalf("%+v", c)
	}

	joinAt(a, t0, eu, "TNL-001")
	if d := a.SetCard(shotFor(t, portal164, t0.Add(time.Second)), zonecard.Default()); d != "портал TNL-001 → TNL-164 отправляю" {
		t.Fatal(d)
	}
	if len(m.tips) != 1 || m.tips[0].From != "TNL-001" || m.tips[0].To != "TNL-164" || m.tips[0].Region != "europe" {
		t.Fatalf("%+v", m.tips)
	}
	c = a.State().Card
	if c.Map == nil || c.Map.Result != avalon.ResOK || c.MapWhy != "" {
		t.Fatalf("%+v", c)
	}

	// Отправка выключена — «off», ничего не уходит.
	s := a.Settings()
	s.MapSend = false
	a.SetSettings(s)
	if d := a.SetCard(shotFor(t, portal164, t0.Add(2*time.Second)), zonecard.Default()); d != "не отправлено (off)" {
		t.Fatal(d)
	}
	if len(m.tips) != 1 || a.State().Card.MapWhy != "off" || a.State().Card.Map != nil {
		t.Fatalf("%+v", a.State().Card)
	}

	// Ошибка снимка — карточка с кодом беды.
	if d := a.SetCard(zonecard.Shot{Kind: zonecard.ErrKindNoTooltip}, zonecard.Default()); d != "не отправлено (noTooltip)" {
		t.Fatal(d)
	}
	if c := a.State().Card; c.Error != zonecard.ErrKindNoTooltip || c.Zone != nil {
		t.Fatalf("%+v", c)
	}
}

func TestCardRiskAndWantForFailures(t *testing.T) {
	dir := t.TempDir()
	a := New(dir, dir, nil)
	t0 := time.Unix(5000, 0)
	joinAt(a, t0, eu, "TNL-001")
	ok := func(sec int, alive float64) {
		a.mu.Lock()
		a.onTransition(zones.Transition{T: t0.Add(time.Duration(sec) * time.Second), From: "TNL-001", To: "TNL-164", OK: true, ReplySec: 0.5, AliveSec: alive})
		a.mu.Unlock()
	}
	fail := func(sec int) {
		a.mu.Lock()
		a.onTransition(zones.Transition{T: t0.Add(time.Duration(sec) * time.Second), From: "TNL-001", OK: false, ReplySec: -1, AliveSec: -1})
		a.mu.Unlock()
	}
	ok(10, 0.3)
	ok(20, 15) // сервер молчал после входа
	a.SetCard(shotFor(t, portal164, t0.Add(25*time.Second)), zonecard.Default())
	if c := a.State().Card; c.Total != 2 || c.Risk {
		t.Fatalf("двух переходов мало: %+v", c)
	}
	// Сняли портал и вылетели по дороге — вылет засчитан этой зоне.
	fail(30)
	c := a.State().Card
	if c.Black != 2 || c.Total != 3 || !c.Risk {
		t.Fatalf("%+v", c)
	}
	if a.trs[len(a.trs)-1].Want != "TNL-164" {
		t.Fatal("Want не записан", a.trs[len(a.trs)-1])
	}
	// Второй вылет без нового снимка — цель неизвестна.
	fail(40)
	if c := a.State().Card; c.Total != 3 {
		t.Fatalf("%+v", c)
	}
	// Предупреждения выключены — риск не показываем.
	s := a.Settings()
	s.BlackWarn = false
	a.SetSettings(s)
	if a.State().Card.Risk {
		t.Fatal("выключено")
	}
}

func TestStallNotifiesOnlyWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	a := New(dir, dir, nil)
	got := make(chan string, 4)
	a.OnStall(func(server, from string) { got <- server + "|" + from })
	t0 := time.Unix(5000, 0)
	joinAt(a, t0, eu, "TNL-001")
	a.mu.Lock()
	a.tracker.On(gameEv(t0.Add(10*time.Second), "cc", eu))
	a.tracker.On(gameEv(t0.Add(10*time.Second), "connect", "193.169.238.99:5056"))
	a.mu.Unlock()
	a.CheckStall(t0.Add(13 * time.Second))
	select {
	case g := <-got:
		if g != "193.169.238.99:5056|"+a.name("TNL-001") {
			t.Fatal(g)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("нет предупреждения")
	}
	s := a.Settings()
	s.BlackWarn = false
	a.SetSettings(s)
	a.mu.Lock()
	a.tracker.On(gameEv(t0.Add(20*time.Second), "join", "193.169.238.99:5056"))
	a.tracker.On(gameEv(t0.Add(30*time.Second), "cc", "193.169.238.99:5056"))
	a.tracker.On(gameEv(t0.Add(30*time.Second), "connect", "193.169.238.98:5056"))
	a.mu.Unlock()
	a.CheckStall(t0.Add(40 * time.Second))
	select {
	case g := <-got:
		t.Fatal("выключено, а предупредили:", g)
	case <-time.After(100 * time.Millisecond):
	}
}

func gameEv(at time.Time, kind, server string) game.Ev {
	k := map[string]game.Kind{"cc": game.ChangeCluster, "connect": game.Connect, "join": game.Join}[kind]
	e := game.Ev{T: at, Kind: k, Server: server}
	if k == game.Join {
		e.Location = "TNL-002"
	}
	return e
}

// Нестабильный путь (в один конец, время «для вашей группы») на общую
// карту не идёт: в журнале «не отправлено (unstable)», на карточке — метка.
func TestCardUnstableNotSent(t *testing.T) {
	dir := t.TempDir()
	a := New(dir, dir, nil)
	m := &fakeMap{}
	a.AttachMap(m, nil)
	t0 := time.Unix(5000, 0)
	joinAt(a, t0, eu, "TNL-001")
	unstable := []string{"Unstable Roads to", "Qiient-Al-Vynsis", "Closes to your party in 4 m 18 s"}
	if d := a.SetCard(shotFor(t, unstable, t0.Add(time.Second)), zonecard.Default()); d != "не отправлено (unstable)" {
		t.Fatal(d)
	}
	c := a.State().Card
	if len(m.tips) != 0 || c.MapWhy != zonecard.WhyUnstable || !c.Unstable || c.Zone == nil || c.Zone.Code != "TNL-164" {
		t.Fatalf("%+v %+v", c, m.tips)
	}
	// Обычный портал после него — снова уходит.
	if d := a.SetCard(shotFor(t, portal164, t0.Add(2*time.Second)), zonecard.Default()); d != "портал TNL-001 → TNL-164 отправляю" {
		t.Fatal(d)
	}
}
