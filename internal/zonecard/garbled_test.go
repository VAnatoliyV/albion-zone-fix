package zonecard

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"albionzonefix/internal/settings"
)

// Журнал тестера на 1.0.8 (Windows 10, русский клиент, 1080p): тултип
// портала дорог прямо в игре, не на карте мира. Строки — что вернул
// Windows.Media.Ocr («прочитано» в журнале), по языкам движка.
var inGame = []struct {
	en, ru []string
	// marker — признак находится хотя бы на одном языке.
	marker bool
}{
	{en: []string{"ABan0H0 a", "GreenhoUwVaIe"}, ru: []string{"аволоно в", "СгеепЬоПмК1е"}, marker: true},
	{en: []string{"n AnanoH0 B", "HW.tme Grasskmd", `—.15 "40M`}, ru: []string{"П Жалоно в", "Grosskmd"}, marker: true},
	{en: []string{"—nyTbAaan0H0 B", "6 q 17 N"}, ru: []string{"—лутьАволоно в", "чгр•п б Ч 17 М"}, marker: true},
	{en: []string{"vrg•e• 6 q 17 M"}, ru: []string{"ЛгьАвплона в", "Х б ч 17 М"}, marker: true},
	{en: []string{"1bAeanoHa e", "17"}, ru: []string{":ьАвалона в", "17 Н"}, marker: true},
	{en: []string{"nyTb AeanoH0 a", "2 5 26 C"}, ru: []string{"Путь Авалоно в"}, marker: true},
	// Заголовок не попал в снимок вовсе — признака нет ни на одном языке.
	{en: []string{"HWtone Grcssk•nd", "39 M"}, ru: []string{"HOstone GrtBskIBd"}, marker: false},
}

func TestGarbledMarker(t *testing.T) {
	for i, c := range inGame {
		got := HasMarker(c.en) || HasMarker(c.ru)
		if got != c.marker {
			t.Errorf("строка %d (%q / %q): признак %v, ждали %v", i+1, c.en, c.ru, got, c.marker)
		}
	}
	// По отдельности: каждый русский вариант строк 1–6 и английские, где в
	// заголовке хоть что-то от «Путь Авалона в».
	for _, l := range []string{"аволоно в", "П Жалоно в", "—лутьАволоно в", "ЛгьАвплона в", ":ьАвалона в", "Путь Авалоно в",
		"ABan0H0 a", "n AnanoH0 B", "—nyTbAaan0H0 B", "1bAeanoHa e", "nyTb AeanoH0 a"} {
		if !HasMarker([]string{l}) {
			t.Errorf("признак не найден: %q", l)
		}
	}
}

func TestGarbledMarkerNoFalse(t *testing.T) {
	for _, l := range [][]string{
		{"Avalonian Chest", "T6"},
		{"Avalonian Chest", "Закроется через 6 ч 26 м"},
		{"Биом Пути Авалона"},
		{"Биом", "Пути Авалона"},
		{"Тип зоны Черный регион", "Уровень VI"},
		{"Hello world", "Inventory", "Silver 12 345"},
		{"Путь", "Fleos"},
		{"Avalon", "Roads"},
		{"Brecilien", "Закроется через 6 ч 26 м"},
		{"Battlebrae Grassland", "T5 a"},
		{"Ava", "a"},
		{"Аванпост в", "Avalonian Elite"},
	} {
		if HasMarker(l) {
			t.Errorf("лишний признак: %q", l)
		}
	}
}

func TestTolerantTime(t *testing.T) {
	h, m, s := time.Hour, time.Minute, time.Second
	for _, c := range []struct {
		line string
		want time.Duration
	}{
		{"6 q 17 N", 6*h + 17*m},
		{"б Ч 17 М", 6*h + 17*m},
		{"чгр•п б Ч 17 М", 6*h + 17*m},
		{"Х б ч 17 М", 6*h + 17*m},
		{"vrg•e• 6 q 17 M", 6*h + 17*m},
		{`—.15 "40M`, 15*h + 40*m}, // две единицы, вторая — минуты: первая — часы
		{"2 5 26 C", 0},            // три числа на две единицы — неоднозначно
		{"39 M", 0},                // одни минуты — скорее обрезанное «X ч 39 м»
		{"17 Н", 0},
		{"17", 0},
		{"7/7", 0},
		{"Закроется через 7 ч 05 м", 7*h + 5*m},
		{"Закроется для вашей группы через 4 м 18 с", 4*m + 18*s},
		{"Closes in 49 m 27 s", 49*m + 27*s},
		{"Закроется через 6 4 17 M", 6*h + 17*m},
		{"6 q 77 N", 0}, // минут больше 59 — не время
	} {
		if got := tolerantTime(c.line); got != c.want {
			t.Errorf("%q: %v, ждали %v", c.line, got, c.want)
		}
	}
}

// Время из тултипа целиком: искажённое понимается только при признаке.
func TestGarbledTooltipTime(t *testing.T) {
	tt, ok := ParseTooltip([]string{"—nyTbAaan0H0 B", "Pasos-Avosam", "6 q 17 N"})
	if !ok || tt.Left != 6*time.Hour+17*time.Minute {
		t.Fatalf("%+v %v", tt, ok)
	}
	// Без признака «6 q 17 N» — не время (и не тултип).
	if _, ok := ParseTooltip([]string{"Pasos-Avosam", "6 q 17 N"}); ok {
		t.Fatal("без признака принят тултип")
	}
	if timeLeft([]string{"6 q 17 N"}) != 0 {
		t.Fatal("строгое время не понимает «q»")
	}
}

func TestLooseNames(t *testing.T) {
	d := dict(t)
	for _, c := range []struct{ read, want string }{
		{"GreenhoUwVaIe", "Greenhollow Vale"},
		{"СгеепЬоПмК1е", "Greenhollow Vale"},
		{"HW.tme Grasskmd", "Highstone Grassland"},
		{"HWtone Grcssk•nd", "Highstone Grassland"},
		{"HOstone GrtBskIBd", "Highstone Grassland"},
	} {
		m := d.SimilarLoose(Clean(c.read), 3)
		if len(m) == 0 || m[0].Zone.Name != c.want || !looseOK(m) {
			t.Errorf("%q: %v", c.read, describe(m))
		}
	}
}

func describe(m []Match) string {
	var out []string
	for _, x := range m {
		out = append(out, fmt.Sprintf("%s %.2f", x.Zone.Name, x.Closeness))
	}
	return strings.Join(out, ", ")
}

// Признак найден, название строго не опознано — нестрогое, сомнительное.
func TestIdentifyGarbledInGame(t *testing.T) {
	d := dict(t)
	at := time.Unix(1_800_000_000, 0)
	for _, c := range []struct {
		lines []string
		want  string
	}{
		{inGame[0].en, "Greenhollow Vale"},
		{inGame[0].ru, "Greenhollow Vale"},
		{inGame[1].en, "Highstone Grassland"},
		{[]string{"n AnanoH0 B", "HWtone Grcssk•nd"}, "Highstone Grassland"},
		{[]string{"П Жалоно в", "HOstone GrtBskIBd"}, "Highstone Grassland"},
	} {
		r, err := Identify(d, c.lines, at)
		if err != nil || !r.Portal || r.Zone() == nil || r.Zone().Name != c.want {
			t.Errorf("%q: %+v %v", c.lines, r, err)
			continue
		}
		if !r.Doubtful() {
			t.Errorf("%q: нестрогое название не помечено сомнительным", c.lines)
		}
	}
	// Пара строки 2: на английском — Highstone Grassland, русское «Grosskmd»
	// одно не различает «… Grassland» — выбирается английский.
	r, err := Choose(d, map[string][]string{"en-US": inGame[1].en, "ru": inGame[1].ru}, []string{"ru", "en-US"}, at)
	if err != nil || r.Zone().Name != "Highstone Grassland" || r.Tooltip.Left != 15*time.Hour+40*time.Minute || !r.Doubtful() {
		t.Errorf("пара строки 2: %+v %v", r, err)
	}
	// Признак есть, а название — мусор: зона не узнана (не карточка).
	_, err = Identify(d, inGame[2].ru, at)
	var u UnknownError
	if !errors.As(err, &u) {
		t.Errorf("мусор вместо названия: %v", err)
	}
}

// Ещё снимки того же тестера: удачные остаются удачными, сомнительные —
// сомнительными, а мусор — «не узнал зону», а не карточка.
func TestTesterNamesFloor(t *testing.T) {
	d := dict(t)
	at := time.Unix(1_800_000_000, 0)
	id := func(name string) (Result, error) {
		return Identify(d, []string{"Путь Авалона в", name, "Закроется через 6 ч 26 м"}, at)
	}
	for _, c := range []struct {
		read, want string
		doubt      bool
	}{
		{"Tims-Odoxlum", "Tiros-Odoxlum", false},
		{"Tiros-OdoxIum", "Tiros-Odoxlum", false},
		{"Tiros-Odoxlum", "Tiros-Odoxlum", false},
		{"Dryvein", "Dryvein End", true},
		{"Forsbore соре", "Farshore Cape", true},
	} {
		r, err := id(c.read)
		if err != nil || r.Zone().Name != c.want || r.Doubtful() != c.doubt {
			t.Errorf("%q: %+v %v", c.read, r, err)
		}
	}
	// «0ИТ-Еготшт» → Ouyos-Aoeuam (0.08) показывалось карточкой.
	_, err := id("0ИТ-Еготшт")
	var u UnknownError
	if !errors.As(err, &u) {
		t.Fatalf("ниже порога — не узнано: %v", err)
	}
	sh := Shot{Kind: ErrKindUnknown, Arg: u.Read}
	if p := Present("ru", sh, settings.Default(), at, true); p.Panel != nil || p.Toast != nil {
		t.Fatal("не узнанная зона показана карточкой")
	}
}

func TestWeakMarker(t *testing.T) {
	for _, l := range [][]string{{"vrg•e•", "Авал"}, {"aval"}, {"nyTb AeanoH"}, {"—лутьАволоно"}} {
		if !WeakMarker(l) {
			t.Errorf("нет слабого признака: %q", l)
		}
	}
	for _, l := range [][]string{{"Inventory"}, {"HWtone Grcssk•nd", "39 M"}, {"Биом Пути Авалона"}} {
		if WeakMarker(l) {
			t.Errorf("лишний слабый признак: %q", l)
		}
	}
}

// Слабый признак при noTooltip — сначала варианты этого снимка, потом новый.
func TestRunnerWeakMarkerTriesVariants(t *testing.T) {
	r, calls, _ := retryRunner(t, map[string][]string{
		"/d/zone-capture.png":     {"vrg•e•", "AeanoH"}, // обрывок «Авалон»
		"/d/zone-capture-inv.png": {"Путь Авалона в", "Fleos-Aluttum", "Закроется через 6 ч 26 м"},
	}, nil)
	s := r.Run(context.Background())
	if !Good(s) || s.Try != "1/3 инверсия" {
		t.Fatalf("%+v", s)
	}
	if strings.Join(*calls, "|") != "/d/zone-capture.png|/d/zone-capture-gray.png|/d/zone-capture-inv.png" {
		t.Fatalf("порядок: %v", *calls)
	}
}

// Обрезано по тултипу, а на обрезке пусто — вся рамка, потом новый снимок.
func TestRunnerCroppedFallsBackToFullFrame(t *testing.T) {
	r, calls, _ := retryRunner(t, map[string][]string{
		"/d/zone-capture-full.png": {"Путь Авалона в", "Fleos-Aluttum", "Закроется через 6 ч 26 м"},
	}, nil)
	r.cfg.Capture = func(p string) (Snap, error) { return Snap{Info: p, Cropped: true}, nil }
	s := r.Run(context.Background())
	if !Good(s) || s.Try != "1/3 вся рамка" {
		t.Fatalf("%+v", s)
	}
	if strings.Join(*calls, "|") != "/d/zone-capture.png|/d/zone-capture-full.png" {
		t.Fatalf("порядок: %v", *calls)
	}
}

// Подсказка «точнее всего на карте мира (M)»: при сомнительном или не
// узнанном тултипе, не чаще раза в MapHintEvery.
func TestMapHint(t *testing.T) {
	d := dict(t)
	at := time.Unix(1_800_000_000, 0)
	r, err := Identify(d, inGame[0].en, at)
	if err != nil {
		t.Fatal(err)
	}
	doubt := Shot{Result: r}
	unknown := Shot{Kind: ErrKindUnknown, Arg: "чгрп"}
	weak := Shot{Kind: ErrKindNoTooltip, Weak: true}
	for _, s := range []Shot{doubt, unknown, weak} {
		if !WantsMapHint(s) {
			t.Errorf("подсказка нужна: %+v", s)
		}
	}
	sure, _ := Identify(d, []string{"Путь Авалона в", "Fleos-Aluttum", "Закроется через 6 ч 26 м"}, at)
	if WantsMapHint(Shot{Result: sure}) || WantsMapHint(Shot{Kind: ErrKindNoTooltip}) {
		t.Error("лишняя подсказка")
	}

	set := settings.Default()
	set.ZoneShow = settings.ShowPanel
	p := PresentWith("ru", doubt, set, at, true, true)
	if p.Panel == nil || !strings.HasSuffix(p.Panel.Title, " ?") || !strings.Contains(p.Panel.Doubt, "карте мира (M)") {
		t.Fatalf("панель: %+v", p.Panel)
	}
	p = PresentWith("en", unknown, set, at, false, true)
	if p.Toast == nil || !strings.Contains(p.Toast.Body, "world map (M)") {
		t.Fatalf("уведомление: %+v", p)
	}
	if p = PresentWith("es", unknown, set, at, true, false); p.Panel != nil || p.Toast != nil {
		t.Fatal("без подсказки не узнанное не показываем")
	}
	set.ZoneShow = settings.ShowNotify
	if p = PresentWith("es", doubt, set, at, true, true); p.Toast == nil || !strings.Contains(p.Toast.Body, "mapa del mundo (M)") || !strings.HasSuffix(p.Toast.Title, " ?") {
		t.Fatalf("es: %+v", p.Toast)
	}

	var g HintGate
	if !g.Allow(at) || g.Allow(at.Add(time.Minute)) || !g.Allow(at.Add(MapHintEvery)) {
		t.Error("не чаще раза в MapHintEvery")
	}
}

// Признак есть, названия нет вовсе — «не узнал зону», а не «не портал».
func TestMarkerWithoutName(t *testing.T) {
	d := dict(t)
	for _, l := range [][]string{inGame[4].en, inGame[5].ru, inGame[3].ru} {
		_, err := Identify(d, l, time.Unix(1000, 0))
		var u UnknownError
		if !errors.As(err, &u) {
			t.Errorf("%q: %v", l, err)
		}
	}
}
