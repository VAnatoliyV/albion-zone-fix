package zonecard

import (
	"strings"
	"testing"
	"time"
)

// Проверки app/проверка-зоны.swift мак-версии — те же строки, распознанные
// с экрана 24 сентября 2026, и те же ожидания.

func mustParse(t *testing.T, lines []string) Tooltip {
	t.Helper()
	tt, ok := ParseTooltip(lines)
	if !ok {
		t.Fatalf("не разобралось: %q", lines)
	}
	return tt
}

func TestTooltipFirstShot(t *testing.T) {
	// Первый живой снимок: «7/7» Vision не прочитал, полоску увидел как «+2 n/a».
	tt := mustParse(t, []string{"n", "Road of Avalon to", "Secent-Al-Qinsom", "+2 n/a", "¿ Closes in 49 m 27 s"})
	if tt.Read != "Secent-Al-Qinsom" {
		t.Errorf("название из первого снимка: %q", tt.Read)
	}
	if tt.Left != 49*time.Minute+27*time.Second {
		t.Errorf("время 49 м 27 с: %v", tt.Left)
	}
	if tt.Size != 0 {
		t.Errorf("размер не выдуман: %d", tt.Size)
	}
}

func TestTooltipSecondShot(t *testing.T) {
	// Второй: часы вместо минут, полоска прочиталась.
	tt := mustParse(t, []string{"Road of Avalon to", "Qiient-Al-Vynsis", "7/7", "n/a", "Closes in 5 h 53 m"})
	if tt.Read != "Qiient-Al-Vynsis" || tt.Size != 7 || tt.Left != 5*time.Hour+53*time.Minute {
		t.Errorf("%+v", tt)
	}
}

func TestTooltipRussian(t *testing.T) {
	// Русский клиент: «Путь Авалона в».
	tt := mustParse(t, []string{"Путь Авалона в", "Cebos-Avemlum", "7/7", "Закроется через 23 м 14 с"})
	if tt.Read != "Cebos-Avemlum" || tt.Left != 23*time.Minute+14*time.Second {
		t.Errorf("%+v", tt)
	}
}

func TestTooltipSkullSameLine(t *testing.T) {
	// Череп перед названием и прочий мусор распознавания.
	tt := mustParse(t, []string{"Road of Avalon to ☠ Secent-Al-Qinsom", "👤 7/7", "⏳ Closes in 48 m 19 s"})
	if tt.Read != "Secent-Al-Qinsom" {
		t.Errorf("название в одной строке с признаком: %q", tt.Read)
	}
}

func TestTooltipRussianSameLine(t *testing.T) {
	// Название в одной строке после русского предлога, время в часах кириллицей.
	tt := mustParse(t, []string{"Путь Авалона в Soritos-Apenium", "7/7", "+ нет", "Закроется через 9 ч 32 м"})
	if tt.Read != "Soritos-Apenium" {
		t.Errorf("русский предлог в той же строке: %q", tt.Read)
	}
	if tt.Left != 9*time.Hour+32*time.Minute {
		t.Errorf("русское время 9 ч 32 м: %v", tt.Left)
	}
	if tt.Size != 7 {
		t.Errorf("размер 7 при русском тултипе: %d", tt.Size)
	}
}

func TestTooltipRejects(t *testing.T) {
	if _, ok := ParseTooltip([]string{"Flimmerair Steppe", "T6", "Outpost x3"}); ok {
		t.Error("посторонний текст отвергнут")
	}
	if _, ok := ParseTooltip(nil); ok {
		t.Error("пустота отвергнута")
	}
}

func dict(t *testing.T) *Dict {
	t.Helper()
	d := Default()
	if d == nil {
		t.Fatal("не прочитал справочник")
	}
	return d
}

func TestDictionary(t *testing.T) {
	d := dict(t)
	if len(d.Zones) <= 1200 {
		t.Errorf("зон больше тысячи: %d", len(d.Zones))
	}
	roads := 0
	for _, z := range d.Zones {
		if z.Road {
			roads++
		}
	}
	if roads != 400 {
		t.Errorf("дорог ровно 400: %d", roads)
	}

	// Обычная зона: тир и качество берутся из другого места, чем у дорог.
	o := d.Exact("Flimmerair Steppe")
	if o == nil {
		t.Fatal("Flimmerair Steppe не найдена")
	}
	if o.Tier != 6 || o.Quality != "red" || o.Road || o.Res["HIDE"] == nil || len(o.Chests) != 0 {
		t.Errorf("открытый мир: %+v", o)
	}

	z := d.Exact("Qiient-Al-Vynsis")
	if z == nil {
		t.Fatal("Qiient-Al-Vynsis не найдена")
	}
	if z.Code != "TNL-164" || z.Tier != 6 {
		t.Errorf("точный поиск: %s T%d", z.Code, z.Tier)
	}
	if strings.Join(z.Points, ",") != "FIBER" {
		t.Errorf("точки на карте — одна, волокно: %v", z.Points)
	}
	if z.Res["HIDE"] == nil || strings.Contains(strings.Join(z.Points, ","), "HIDE") {
		t.Error("шкуры есть в узлах, хотя точки у них нет")
	}
	if z.Camps["group"] != 2 {
		t.Errorf("лагеря: 2 групповых: %v", z.Camps)
	}
	if z.Chests["medium_veteran"] != 2 {
		t.Errorf("сундуки: 2 средних ветеранских: %v", z.Chests)
	}

	// Ошибка распознавания: «Ai» вместо «Al» — так пользователь и прочитал глазами.
	m := d.Similar("Secent-Ai Qinsom", 3)
	if len(m) == 0 || m[0].Zone.Name != "Secent-Al-Qinsom" {
		t.Fatalf("опечатка опознана как Secent-Al-Qinsom: %v", m)
	}
	if m[0].Closeness <= 0.8 {
		t.Errorf("близость выше 0.8: %v", m[0].Closeness)
	}
	if len(m) != 3 || !strings.Contains(m[1].Zone.Name, "Qins") {
		t.Errorf("близнецы не потеряны — их видно вторым и третьим: %s, %s", m[1].Zone.Name, m[2].Zone.Name)
	}

	// Пара зон: одно имя — одна карта.
	a, b := d.Exact("Secent-Al-Qinsom"), d.Exact("Secent-Qi-Qinsom")
	if a == nil || b == nil || strings.Join(keys(a.Res), ",") != strings.Join(keys(b.Res), ",") || a.Tier != b.Tier {
		t.Error("парные зоны совпадают по составу")
	}
}

func keys(m map[string][][]int) []string { return sortedKeys(m) }

// Русский движок OCR Windows может прочитать латинское название похожими
// русскими буквами — опознание сводит их к латинице.
func TestCyrillicLookalikes(t *testing.T) {
	d := dict(t)
	m := d.Similar("Сеbоs-Аvеmlum", 3) // С, е, о, А, е — кириллица
	if len(m) == 0 || m[0].Zone.Name != "Cebos-Avemlum" || m[0].Closeness != 1 {
		t.Fatalf("%v", m)
	}
	if FoldLatin("Закроется") != "Закроется" {
		t.Error("настоящее русское слово не трогаем")
	}
}

func TestIdentifyAndDoubt(t *testing.T) {
	d := dict(t)
	at := time.Unix(1000, 0)
	r, err := Identify(d, []string{"Road of Avalon to", "Qiient-Al-Vynsis", "7/7", "Closes in 5 h 53 m"}, at)
	if err != nil || !r.Portal || r.Zone().Code != "TNL-164" || r.Doubtful() {
		t.Fatalf("%+v %v", r, err)
	}
	if !r.ClosesAt().Equal(at.Add(5*time.Hour + 53*time.Minute)) {
		t.Error(r.ClosesAt())
	}
	if left, ok := r.LeftAt(at.Add(time.Hour)); !ok || left != 4*time.Hour+53*time.Minute {
		t.Error(left)
	}
	// Опечатка с близнецами рядом — сомнение.
	r, _ = Identify(d, []string{"Road of Avalon to", "Secent-Ai Qinsom"}, at)
	if r.Zone() == nil || r.Zone().Name != "Secent-Al-Qinsom" {
		t.Fatal(r)
	}
	// Тултип есть, а зону не узнать — так и говорим.
	if _, err := Identify(NewDict(nil), []string{"Road of Avalon to", "Qiient-Al-Vynsis"}, at); err == nil {
		t.Fatal("пустой справочник")
	} else if _, ok := err.(UnknownError); !ok {
		t.Fatal(err)
	}
	// Без портала — строгое опознание по названию среди текста карты.
	r, err = Identify(d, []string{"T6", "Flimmerair Steppe", "Outpost x3"}, at)
	if err != nil || r.Portal || r.Zone().Name != "Flimmerair Steppe" {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := Identify(d, []string{"Inventory", "Silver 12 345"}, at); err != ErrNoTooltip {
		t.Fatal(err)
	}
}

func TestChooseLanguage(t *testing.T) {
	d := dict(t)
	at := time.Unix(1000, 0)
	// Русский клиент: английский движок прочитал кириллицу мусором, русский —
	// нормально. Берётся русский.
	by := map[string][]string{
		"en-US": {"llyTb ABaIoHa B", "Cebos-Avemlum", "7/7"},
		"ru-RU": {"Путь Авалона в", "Сеbоs-Avemlum", "7/7", "Закроется через 23 м 14 с"},
	}
	r, err := Choose(d, by, []string{"ru-RU", "en-US"}, at)
	if err != nil || r.Lang != "ru-RU" || r.Zone().Name != "Cebos-Avemlum" || r.Tooltip.Left != 23*time.Minute+14*time.Second {
		t.Fatalf("%+v %v", r, err)
	}
	// Английский клиент: тултип нашёлся на обоих — берётся увереннее опознанный.
	by = map[string][]string{
		"ru-RU": {"Road of Avalon to", "Qiient-AI-Vуnsiс"},
		"en-US": {"Road of Avalon to", "Qiient-Al-Vynsis", "Closes in 5 h 53 m"},
	}
	r, err = Choose(d, by, []string{"ru-RU", "en-US"}, at)
	if err != nil || r.Lang != "en-US" || r.Zone().Code != "TNL-164" {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := Choose(d, map[string][]string{"en-US": {"hello"}}, []string{"ru-RU", "en-US"}, at); err != ErrNoTooltip {
		t.Fatal(err)
	}
}

// Живые снимки тестера (Windows 10, русский клиент, 8 октября 2026):
// мелкий серый заголовок Windows OCR читает с ошибкой в букве — признак
// должен находиться и так.
func TestTooltipMarkerWithOCRTypos(t *testing.T) {
	for _, head := range []string{"Путь Авапона в", "Пvть Авалона в", "Путь Аваnона в", "Road of Avaion to", "Rood of Avalon to"} {
		tt := mustParse(t, []string{head, "Fleos-Aluttum", "нет", "Закроется через 6 ч 26 м"})
		if tt.Read != "Fleos-Aluttum" || tt.Left != 6*time.Hour+26*time.Minute {
			t.Errorf("%q: %+v", head, tt)
		}
	}
}

// «Нестабильные Пути в» — портал в один конец, время «для вашей группы».
func TestTooltipUnstableRoads(t *testing.T) {
	tt := mustParse(t, []string{"Нестабильные Пути в", "Poues-Unatam", "6/7", "Тип зоны Черный регион",
		"Биом Пути Авалона", "Уровень VI", "Это переход в один конец!", "Закроется для вашей группы через 4 м 18 с"})
	if tt.Read != "Poues-Unatam" || tt.Size != 7 || tt.Left != 4*time.Minute+18*time.Second {
		t.Errorf("%+v", tt)
	}
	tt = mustParse(t, []string{"Unstable Roads to", "Poues-Unatam", "6/7", "Closes for your group in 4 m 18 s"})
	if tt.Read != "Poues-Unatam" || tt.Left != 4*time.Minute+18*time.Second {
		t.Errorf("англ.: %+v", tt)
	}
}

// Заголовок не прочитан вовсе, но есть название дороги и «Закроется через» —
// это тултип портала (иначе выходило noPortal без времени).
func TestIdentifyRoadNameWithTimeIsPortal(t *testing.T) {
	d := dict(t)
	r, err := Identify(d, []string{"• Pasos-Avosam", "нет", "2 Закроется через 6 ч 26 м"}, time.Unix(1_800_000_000, 0))
	if err != nil || !r.Portal || r.Tooltip.Left != 6*time.Hour+26*time.Minute || r.Zone() == nil || !r.Zone().Road {
		t.Fatalf("%+v %v", r, err)
	}
	// Название города со временем чего-то другого — не портал.
	r, err = Identify(d, []string{"Brecilien", "Закроется через 6 ч 26 м"}, time.Unix(1_800_000_000, 0))
	if err == nil && r.Portal {
		t.Fatalf("город с временем принят за портал: %+v", r)
	}
}

func TestTooltipFuzzyDoesNotOvermatch(t *testing.T) {
	for _, l := range [][]string{{"Avalonian Chest", "T6"}, {"Путь", "Fleos"}, {"Avalon", "Roads"}} {
		if _, ok := ParseTooltip(l); ok {
			t.Errorf("принято лишнее: %q", l)
		}
	}
}
