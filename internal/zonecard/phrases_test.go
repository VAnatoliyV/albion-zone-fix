package zonecard

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"albionzonefix/internal/avalon"
	"albionzonefix/internal/i18n"
)

// Синтетические тултипы на каждом языке клиента игры — из тех же фраз
// tooltip-phrases.json (данные игры): заголовок + название + «закроется
// через 5 ч 3 м» в форме языка.

var ourLangs = []string{"ru", "en", "es", "pl", "de", "tr", "fr", "pt", "it"}

func phrasesFor(t *testing.T) map[string]langPhrases {
	t.Helper()
	var all map[string]langPhrases
	if err := json.Unmarshal(phrasesJSON, &all); err != nil {
		t.Fatal(err)
	}
	for _, l := range ourLangs {
		p, ok := all[l]
		if !ok || len(p.Marker) == 0 || len(p.Unstable) == 0 || len(p.Closes) == 0 || len(p.Units["h"]) == 0 || len(p.Units["m"]) == 0 {
			t.Fatalf("%s: в tooltip-phrases.json нет фраз: %+v", l, p)
		}
	}
	return all
}

// timeText — «5 h 3 m» в единицах языка.
func timeText(p langPhrases) string {
	return "5 " + p.Units["h"][0] + " 3 " + p.Units["m"][0]
}

// closesLine — строка времени: фраза до или после времени, как в игре
// («Closes in {0}», «{0} içerisinde kapanacaktır»).
func closesLine(phrase, tm string, after bool) string {
	if after {
		return tm + " " + phrase
	}
	return phrase + " " + tm
}

func TestTooltipEveryLanguage(t *testing.T) {
	all := phrasesFor(t)
	const name = "Secent-Al-Qinsom"
	want := 5*time.Hour + 3*time.Minute
	for _, l := range ourLangs {
		p := all[l]
		tm := timeText(p)
		for _, title := range append(append([]string(nil), p.Marker...), p.Unstable...) {
			for _, cl := range p.Closes {
				for _, after := range []bool{false, true} {
					// Название в той же строке, что и заголовок.
					same := []string{title + " " + name, "7/7", closesLine(cl, tm, after)}
					// Название отдельной строкой (как на мелком шрифте).
					split := []string{title, name, "7/7", closesLine(cl, tm, after)}
					for _, lines := range [][]string{same, split} {
						tt, ok := ParseTooltip(lines)
						if !ok || tt.Read != name || tt.Size != 7 || tt.Left != want {
							t.Errorf("%s: %q → %+v ok=%v", l, lines, tt, ok)
						}
					}
				}
			}
			// Опознание целиком: признак, зона из справочника, время.
			r, err := Identify(dict(t), []string{title + " " + name, closesLine(p.Closes[0], tm, false)}, time.Unix(1_800_000_000, 0))
			if err != nil || !r.Portal || r.Zone() == nil || r.Zone().Name != name || r.Tooltip.Left != want {
				t.Errorf("%s: Identify(%q): %+v %v", l, title, r, err)
			}
		}
	}
}

// Нестрогое сравнение: признак с одной-двумя ошибками OCR находится на
// каждом языке (как «Авапона», «Rood» у русского и английского).
func TestMarkerFuzzyEveryLanguage(t *testing.T) {
	all := phrasesFor(t)
	for _, l := range ourLangs {
		for _, title := range all[l].Marker {
			r := []rune(title)
			// Одна буква в середине заменена. Предлог оставляем: без него и
			// с ошибкой заголовок неотличим от значения биома («Roads of
			// Avalon») — такое нарочно не признак.
			r[len(r)/2] = 'x'
			bad := string(r)
			if !HasMarker([]string{bad}) {
				t.Errorf("%s: признак с ошибкой %q не найден", l, bad)
			}
		}
	}
}

// «Биом: Пути Авалона» и значение «Пути Авалона» без подписи — не признак
// ни на одном языке.
func TestBiomeIsNotMarker(t *testing.T) {
	all := phrasesFor(t)
	for _, l := range ourLangs {
		p := all[l]
		for _, v := range p.NotMarker {
			for _, lines := range [][]string{{p.Biome[0] + ": " + v}, {v}, {p.Biome[0], v}} {
				if HasMarker(lines) {
					t.Errorf("%s: %q принято за признак", l, lines)
				}
			}
		}
	}
}

// Единицы времени всех языков: «5 st 3 m», «5 sa 3 dk», «1 t 2 st».
func TestTimeUnitsEveryLanguage(t *testing.T) {
	all := phrasesFor(t)
	for _, l := range ourLangs {
		u := all[l].Units
		cl := all[l].Closes[0]
		for _, c := range []struct {
			text string
			want time.Duration
		}{
			{"0 " + u["d"][0] + " 2 " + u["h"][0], 2 * time.Hour},
			// Больше суток портал не живёт — не время.
			{"1 " + u["d"][0] + " 2 " + u["h"][0], 0},
			{"5 " + u["h"][0] + " 3 " + u["m"][0], 5*time.Hour + 3*time.Minute},
			{"49 " + u["m"][0] + " 27 " + u["s"][0], 49*time.Minute + 27*time.Second},
		} {
			line := cl + " " + c.text
			if got := timeLeft([]string{line}); got != c.want {
				t.Errorf("%s: %q → %v, ждал %v", l, line, got, c.want)
			}
		}
	}
	// Индонезийские единицы спорят с остальными (h — дни) и не берутся.
	if _, ok := unitsByLang["id"]; ok {
		t.Error("индонезийские единицы взяты")
	}
}

// Как восточнославянская модель (internal/paddle) читает синтетические
// тултипы de, pl, tr: буквы с диакритикой теряются, но признак, название и
// время опознаются уверенно.
func TestIdentifyEslavDiacritics(t *testing.T) {
	d := dict(t)
	at := time.Unix(1_800_000_000, 0)
	for _, c := range []struct {
		lines []string
		name  string
		left  time.Duration
	}{
		{[]string{"Stralbe von Avalon nach", "Pasos-Avosam", "Schlielt in 6 st 25 m"}, "Pasos-Avosam", 6*time.Hour + 25*time.Minute},
		{[]string{"Instabile Strafbe nach", "Qiient-Al-Vynsis", "Verschlielft sich für deine Gruppe in 4 m 18 s"}, "Qiient-Al-Vynsis", 4*time.Minute + 18*time.Second},
		{[]string{"Sciezka Awalonu do", "Pasos-Avosam", "Zamyka sie za 6 h 25 m"}, "Pasos-Avosam", 6*time.Hour + 25*time.Minute},
		{[]string{"Niestabilne Sciezki do", "Qiient-Al-Vynsis", "Zamyka sie dlla twojej druzyny za 4 m 18 s"}, "Qiient-Al-Vynsis", 4*time.Minute + 18*time.Second},
		{[]string{"Avalon Yolu giki$1", "Secent-Al-Qinsom", "Kapanmasina kalan 6 sa 25 dk"}, "Secent-Al-Qinsom", 6*time.Hour + 25*time.Minute},
		{[]string{"Dengesiz Yol Azi", "Qiient-Al-Vynsis", "4 dk 18 sn igerisinde grubun igin kapatilacaktir"}, "Qiient-Al-Vynsis", 4*time.Minute + 18*time.Second},
	} {
		r, err := Identify(d, c.lines, at)
		if err != nil || !r.Portal || r.Zone() == nil || r.Zone().Name != c.name || r.Doubtful() || r.Tooltip.Left != c.left || r.Tooltip.TimeLoose {
			t.Errorf("%q: %+v %v", c.lines, r, err)
		}
	}
}

// Нестабильный путь — на каждом языке: по заголовку или по строке времени
// «для вашей группы»; обычный путь — не нестабильный. На карту не идёт, в
// уведомлении и панели — метка «в один конец» и время группы.
func TestUnstableEveryLanguage(t *testing.T) {
	all := phrasesFor(t)
	d := dict(t)
	at := time.Unix(1_800_000_000, 0)
	here := &avalon.Place{Zone: "TNL-001", Region: "europe"}
	const name = "Qiient-Al-Vynsis"
	for _, l := range ourLangs {
		p := all[l]
		tm := timeText(p)
		for _, c := range []struct {
			lines []string
			want  bool
		}{
			{[]string{p.Unstable[0], name, closesLine(p.Party[0], tm, l == "tr")}, true},
			{[]string{p.Unstable[0] + " " + name, closesLine(p.Closes[0], tm, false)}, true},
			// Заголовок не прочитан, но время — «для вашей группы».
			{[]string{p.Marker[0], name, closesLine(p.Party[0], tm, l == "tr")}, true},
			{[]string{p.Marker[0], name, closesLine(p.Closes[0], tm, false)}, false},
		} {
			r, err := Identify(d, c.lines, at)
			if err != nil || r.Tooltip.Unstable != c.want {
				t.Errorf("%s: %q → unstable=%v, ждал %v (%v)", l, c.lines, r.Tooltip.Unstable, c.want, err)
				continue
			}
			_, why := ByButton(r, here, d)
			if c.want && why != WhyUnstable || !c.want && why == WhyUnstable {
				t.Errorf("%s: %q → why %q", l, c.lines, why)
			}
			if !c.want {
				continue
			}
			o := ToastOptions{Portal: true}
			toast := BuildToast(l, r.Zone(), r, o, at)
			panel := BuildPanel(l, r.Zone(), r, o, at)
			for _, key := range []string{"zn.unstable", "zn.closesGroup"} {
				want := i18n.T(l, key)
				if key == "zn.closesGroup" {
					want = strings.Split(want, "%s")[0]
				}
				if !strings.Contains(toast.Body, want) || !strings.Contains(panel.Footer, want) {
					t.Errorf("%s: нет %q: %q / %q", l, want, toast.Body, panel.Footer)
				}
			}
		}
	}
}
