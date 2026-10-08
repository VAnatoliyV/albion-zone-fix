package zonecard

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"albionzonefix/internal/avalon"
	"albionzonefix/internal/zones"
)

var allOn = ToastOptions{Chests: true, Res: true, Dungeons: true, Portal: true, ChestsFirst: true}

// Ожидания — то, что для той же зоны собирает enum Уведомление мак-версии
// (Оверлей.swift: строки(з, р), Оформление.подзаголовок(з, со: true)).
func TestToastRoadLikeMac(t *testing.T) {
	z := dict(t).Exact("Qiient-Al-Vynsis") // TUNNEL_HIDEOUT, T6, сундуки medium_veteran 2 + small 1
	at := time.Unix(1000, 0)
	r := Result{Tooltip: Tooltip{Read: z.Name, Size: 7, Left: 5*time.Hour + 53*time.Minute}, At: at, Portal: true}
	got := BuildToast("ru", z, r, allOn, at.Add(7*time.Minute))
	want := Toast{
		Title:    "Qiient-Al-Vynsis",
		Subtitle: "🌀 дорога убежищ · T6",
		Body: "🟦 2 × средний ветеранский   🟩 1 × малый\n" +
			"волокно · шкуры\n" +
			"портал на 7 · закроется через 5 ч 46 м",
	}
	if got != want {
		t.Fatalf("\nесть:  %#v\nждали: %#v", got, want)
	}
	// Ресурсы сверху, без сундуков и портала — как с выключенными переключателями у мака.
	got = BuildToast("en", z, r, ToastOptions{Res: true, Chests: true}, at)
	if got.Body != "fiber · hide\n🟦 2 × medium veteran   🟩 1 × small" {
		t.Fatalf("%q", got.Body)
	}
	// Всё выключено — пустое тело.
	if BuildToast("es", z, r, ToastOptions{}, at).Body != "" {
		t.Fatal("тело должно быть пустым")
	}
}

func TestToastOpenWorldAndGrades(t *testing.T) {
	ow := &Zone{Name: "Somewhere", Quality: "black", Tier: 8, Grade: 3, Biome: []string{"HIDE", "ORE", "WOOD"},
		Res: map[string][][]int{"ROCK": {{8, 4}}}, Dungeons: map[string]int{"solo": 2, "group": 1}}
	r := Result{Tooltip: Tooltip{Read: "Somewhere"}, At: time.Unix(0, 0)}
	got := BuildToast("ru", ow, r, allOn, time.Unix(0, 0))
	if got.Subtitle != "⬛️ чёрная зона · T8 · качество 3" {
		t.Errorf("%q", got.Subtitle)
	}
	// Биом важнее таблицы, основной со звёздочкой; без размера и времени строки портала нет.
	if got.Body != "★ шкуры · руда · дерево\nданжи: 1 × групповое, 2 × соло" {
		t.Errorf("%q", got.Body)
	}
	royal := &Zone{Name: "R", Road: true, Quality: "roads", Type: "TUNNEL_ROYAL", Grade: 2, Tier: 7}
	if s := Subtitle("en", royal, true); s != "🌀 royal II · T7" {
		t.Errorf("%q", s)
	}
	// Обычная дорога словом не подписана: остаётся квадратик и тир.
	low := &Zone{Name: "L", Road: true, Quality: "roads", Type: "TUNNEL_LOW", Grade: 1, Tier: 5}
	if s := Subtitle("ru", low, true); s != "🌀 · T5" {
		t.Errorf("%q", s)
	}
	if s := Subtitle("ru", low, false); s != "T5" {
		t.Errorf("%q", s)
	}
	if Clock("en", 49*time.Minute+27*time.Second) != "49:27" || Clock("es", 2*time.Hour+5*time.Minute) != "2 h 5 min" {
		t.Error(Clock("en", 49*time.Minute+27*time.Second), Clock("es", 2*time.Hour+5*time.Minute))
	}
}

func TestByButton(t *testing.T) {
	d := dict(t)
	at := time.Unix(1_000_000, 400_000_000)
	ok := Result{Tooltip: Tooltip{Read: "Qiient-Al-Vynsis", Size: 7, Left: 49*time.Minute + 27*time.Second},
		Matches: d.Similar("Qiient-Al-Vynsis", 3), At: at, Portal: true}
	here := &avalon.Place{Zone: "TNL-001", Region: "europe"}

	tip, why := ByButton(ok, here, d)
	if why != "" || tip.From != "TNL-001" || tip.To != "TNL-164" || tip.Size != 7 || tip.Region != "europe" ||
		tip.ClosesAt != 1_000_000+49*60+27 { // 0.4 с округляется вниз
		t.Fatalf("%+v %q", tip, why)
	}
	noTime := ok
	noTime.Tooltip.Left = 0
	cases := []struct {
		name string
		r    Result
		here *avalon.Place
		want string
	}{
		{"нет времени", noTime, here, WhyNoTime},
		{"не знаю где", ok, nil, WhyNoPlace},
		{"не Европа", ok, &avalon.Place{Zone: "TNL-001", Region: "asia"}, WhyNotEurope},
		{"сомнительно", Result{Tooltip: ok.Tooltip, Matches: d.Similar("Secent-Ai Qinsom", 3), At: at}, here, WhyDoubt},
		{"тот же", ok, &avalon.Place{Zone: "TNL-164"}, WhySame},
	}
	for _, c := range cases {
		if _, why := ByButton(c.r, c.here, d); why != c.want {
			t.Errorf("%s: %q, ждали %q", c.name, why, c.want)
		}
	}
	// Не дорога: портал из обычной зоны в обычную.
	ow := Result{Tooltip: Tooltip{Read: "Flimmerair Steppe", Left: time.Hour}, Matches: d.Similar("Flimmerair Steppe", 3), At: at}
	steppe := d.Exact("Flimmerair Steppe")
	other := d.Exact("Thetford")
	if _, why := ByButton(ow, &avalon.Place{Zone: other.Code}, d); why != WhyNotRoad {
		t.Errorf("не дорога: %q", why)
	}
	// Из дороги в обычную зону — можно (сервер сам решит), сервер неизвестен — шлём.
	if tip, why := ByButton(ow, &avalon.Place{Zone: "TNL-001"}, d); why != "" || tip.To != steppe.Code || tip.Region != "" {
		t.Errorf("%+v %q", tip, why)
	}
}

func TestRiskAndBlack(t *testing.T) {
	tr := func(to string, ok bool, reply, alive float64, want string) zones.Transition {
		return zones.Transition{To: to, OK: ok, ReplySec: reply, AliveSec: alive, Want: want}
	}
	if IsBlack(tr("A", true, 0.4, 0.2, "")) || IsBlack(tr("A", true, -1, 1, "")) {
		t.Error("обычный переход — не чёрный экран")
	}
	if !IsBlack(tr("", false, -1, -1, "")) || !IsBlack(tr("A", true, 3.5, 0.2, "")) ||
		!IsBlack(tr("A", true, 0.5, 12, "")) || IsBlack(tr("A", true, 0.5, 6, "")) || !IsBlack(tr("A", true, 0.5, -1, "")) {
		t.Error("вылет, долгий ответ, долгое молчание после входа — чёрный экран")
	}
	h := []zones.Transition{
		tr("TNL-164", true, 0.5, 0.3, ""),
		tr("TNL-164", true, 4, 0.3, ""),   // сервер ответил поздно
		tr("", false, -1, -1, "TNL-164"),  // вылет по дороге туда (портал сняли карточкой)
		tr("", false, -1, -1, ""),         // вылет неизвестно куда — не считается
		tr("TNL-001", false, -1, -1, ""),  // другая зона
		tr("TNL-164", true, 0.2, 0.1, ""), //
	}
	n, m := Risk(h, "TNL-164")
	if n != 2 || m != 4 || !ShowRisk(m) {
		t.Fatalf("%d из %d", n, m)
	}
	if _, m := Risk(h[:2], "TNL-164"); ShowRisk(m) {
		t.Error("меньше трёх переходов — не показываем")
	}
	if n, m := Risk(h, ""); n != 0 || m != 0 {
		t.Error("пустой код")
	}
}

func TestRunner(t *testing.T) {
	d := dict(t)
	var shots []Shot
	done := make(chan struct{}, 4)
	ocrLines := map[string][]string{"en-US": {"Road of Avalon to", "Qiient-Al-Vynsis", "7/7", "Closes in 5 h 53 m"}}
	var gotLangs []string
	r := NewRunner(RunnerConfig{
		Path:    "/tmp/x.png",
		Capture: func(string) (string, error) { return "600x400", nil },
		Recognize: func(_ context.Context, _ string, langs []string) (map[string][]string, error) {
			gotLangs = langs
			return ocrLines, nil
		},
		Languages: func(context.Context) ([]string, error) { return []string{"en-US", "de-DE"}, nil },
		Pick: func(in []string) []string {
			var out []string
			for _, l := range in {
				if strings.HasPrefix(l, "en") || strings.HasPrefix(l, "ru") {
					out = append(out, l)
				}
			}
			return out
		},
		Hint: func(in []string) string { return "noRu" },
		Dict: d,
		Done: func(s Shot) { shots = append(shots, s); done <- struct{}{} },
		Gap:  time.Hour,
	})
	r.CheckLanguages(context.Background())
	if l, h, ok := r.OCRStatus(); !ok || h != "noRu" || len(l) != 2 {
		t.Fatal(l, h, ok)
	}
	if !r.Trigger() {
		t.Fatal("первое нажатие")
	}
	<-done
	if len(shots) != 1 || shots[0].Kind != "" || shots[0].Result.Zone().Code != "TNL-164" || strings.Join(gotLangs, ",") != "en-US" {
		t.Fatalf("%+v %v", shots, gotLangs)
	}
	if r.Trigger() {
		t.Fatal("сразу второе нажатие — дребезг, не берём")
	}
	// Ошибки — кодами для страницы.
	r.cfg.Capture = func(string) (string, error) { return "", errors.New("BitBlt") }
	if s := r.Run(context.Background()); s.Kind != ErrKindCapture || s.Arg != "BitBlt" {
		t.Fatalf("%+v", s)
	}
	r.cfg.Capture = func(string) (string, error) { return "", nil }
	ocrLines = map[string][]string{"en-US": {"Road of Avalon to", "Zzzzzzzzzzzzzzzzzzzzzzzzzzzz"}}
	if s := r.Run(context.Background()); s.Kind != ErrKindUnknown && s.Kind != "" {
		t.Fatalf("%+v", s)
	}
	ocrLines = map[string][]string{"en-US": {"Inventory"}}
	if s := r.Run(context.Background()); s.Kind != ErrKindNoTooltip {
		t.Fatalf("%+v", s)
	}
	r.cfg.Recognize = func(context.Context, string, []string) (map[string][]string, error) {
		return nil, errors.New("таймаут")
	}
	if s := r.Run(context.Background()); s.Kind != ErrKindOCR {
		t.Fatalf("%+v", s)
	}
	r.hint = "none"
	if s := r.Run(context.Background()); s.Kind != ErrKindNoLang {
		t.Fatalf("%+v", s)
	}
}

func TestLangPlan(t *testing.T) {
	try := []string{"ru", "en-US"}
	cases := []struct {
		last        string
		first, rest string
	}{
		{"", "ru,en-US", ""},      // язык неизвестен — оба сразу, как раньше
		{"en-US", "en-US", "ru"},  // прошлый удачный — первым
		{"ru", "ru", "en-US"},     //
		{"de-DE", "ru,en-US", ""}, // не среди пробуемых
	}
	for _, c := range cases {
		f, r := LangPlan(try, c.last)
		if strings.Join(f, ",") != c.first || strings.Join(r, ",") != c.rest {
			t.Errorf("%q: %v %v", c.last, f, r)
		}
	}
	if f, r := LangPlan([]string{"en-US"}, "en-US"); len(f) != 1 || r != nil {
		t.Error(f, r)
	}
}

// Английский клиент: русский движок читает название хуже. Первое нажатие —
// оба языка, лучший по оценке (en-US); дальше en-US первым, и русский не
// нужен, пока английский уверен. Без тултипа — второй язык тоже пробуем.
func TestRunnerLangOrder(t *testing.T) {
	d := dict(t)
	en := []string{"Road of Avalon to", "Soros-Axaesum", "7/7", "Closes in 5 h 53 m"}
	ru := []string{"Road of Avalon to", "Sxrxs-Axxxsxm", "7/7", "Closes in 5 h 53 m"}
	screen := map[string][]string{"ru": ru, "en-US": en}
	var calls []string
	r := NewRunner(RunnerConfig{
		Path:    "/tmp/x.png",
		Capture: func(string) (string, error) { return "", nil },
		Recognize: func(_ context.Context, _ string, langs []string) (map[string][]string, error) {
			calls = append(calls, strings.Join(langs, ","))
			out := map[string][]string{}
			for _, l := range langs {
				out[l] = screen[l]
			}
			return out, nil
		},
		Pick: func([]string) []string { return []string{"ru", "en-US"} },
		Dict: d,
	})
	s := r.Run(context.Background())
	if s.Kind != "" || s.Result.Lang != "en-US" || s.Result.Matches[0].Closeness < SureCloseness || strings.Join(calls, "|") != "ru,en-US" {
		t.Fatalf("%+v %v", s.Result, calls)
	}
	calls = nil
	s = r.Run(context.Background())
	if s.Kind != "" || s.Result.Lang != "en-US" || strings.Join(calls, "|") != "en-US" || s.OCRLangs != "en-US" {
		t.Fatalf("%+v %v %q", s.Result, calls, s.OCRLangs)
	}
	// Не портал: на первом языке тултипа нет — пробуем и второй.
	screen = map[string][]string{"ru": {"Инвентарь"}, "en-US": {"Inventory"}}
	calls = nil
	if s = r.Run(context.Background()); s.Kind != ErrKindNoTooltip || strings.Join(calls, "|") != "en-US|ru" || s.OCRLangs != "en-US; ru" {
		t.Fatalf("%+v %v", s, calls)
	}
	// Русский клиент: на английском тултип не уверен — второй язык, и
	// выбирается лучший по оценке; дальше русский первым.
	ruOK := []string{"Путь Авалона в", "Soros-Axaesum", "7/7", "Закроется через 5 ч 53 м"}
	screen = map[string][]string{"ru": ruOK, "en-US": {"Nyrb Agaroha b", "Sxrxs-Axxxsxm"}}
	calls = nil
	if s = r.Run(context.Background()); s.Kind != "" || s.Result.Lang != "ru" || strings.Join(calls, "|") != "en-US|ru" {
		t.Fatalf("%+v %v", s.Result, calls)
	}
	calls = nil
	if s = r.Run(context.Background()); s.Result.Lang != "ru" || strings.Join(calls, "|") != "ru" {
		t.Fatalf("%+v %v", s.Result, calls)
	}
	// Ошибка первого вызова не мешает второму.
	screen = map[string][]string{"en-US": en}
	r.cfg.Recognize = func(_ context.Context, _ string, langs []string) (map[string][]string, error) {
		if langs[0] == "ru" {
			return nil, errors.New("сбой")
		}
		return map[string][]string{"en-US": en}, nil
	}
	if s = r.Run(context.Background()); s.Kind != "" || s.Result.Lang != "en-US" {
		t.Fatalf("%+v", s)
	}
}

func TestDoubtfulShot(t *testing.T) {
	d := dict(t)
	sure := Result{Tooltip: Tooltip{Read: "Soros-Axaesum"}, Matches: d.Similar("Soros-Axaesum", 3), Portal: true}
	weak := Result{Tooltip: Tooltip{Read: "Mawor Согде"}, Matches: d.Similar("Mawor Согде", 3), Portal: true}
	if Doubtful(Shot{Result: sure}) || !Doubtful(Shot{Result: weak}) || !Doubtful(Shot{Kind: ErrKindUnknown}) ||
		Doubtful(Shot{Kind: ErrKindNoTooltip}) || Doubtful(Shot{Result: Result{Matches: weak.Matches}}) {
		t.Fatal(weak.Matches[0].Closeness)
	}
}
