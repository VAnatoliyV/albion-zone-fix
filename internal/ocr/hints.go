package ocr

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"albionzonefix/internal/paddle"
	"albionzonefix/internal/zonecard"
)

// Hints — что может быть написано в тултипе портала, чтобы спросить модель
// прямо, а не только взять самую вероятную букву на каждом шаге:
//   - Names — названия зон: строку, которую модель «видит» как одно из них
//     заметно лучше всех прочих, заменяем точным названием;
//   - TimeRunes — цифры и единицы времени: строку с цифрами читаем ещё раз,
//     разрешив только их, и добавляем найденное время отдельной строкой с
//     пометкой HintTime — разбор тултипа считает его нестрогим (на карту —
//     только если совпало на двух снимках).
//
// Проверено на снимках тестера, уменьшенных до 50% (мелкий текст): верная
// зона первой на всех размерах, время находится до 70%.
type Hints struct {
	Names     []string
	TimeRunes string

	enc   map[string][]int // название → номера символов модели (кэш на словарь)
	chars []string
}

// HintTime — пометка строки времени, найденной чтением только цифр и единиц.
const HintTime = zonecard.HintTime

// Порог названия: насколько вероятность слова может уступать лучшему пути
// модели (на символ), и насколько первое должно обгонять второе.
const (
	nameSlack     = 2.0
	nameSlackRune = 1.3
	nameMargin    = 2.0
	// nameShortlist — сколько ближайших по написанию названий проверять моделью.
	nameShortlist = 40
)

func (h *Hints) prepare(chars []string) {
	if len(h.chars) == len(chars) && h.enc != nil {
		return
	}
	h.chars, h.enc = chars, make(map[string][]int, len(h.Names))
	for _, n := range h.Names {
		if l, ok := paddle.Encode(n, chars); ok && len(l) > 0 {
			h.enc[n] = l
		}
	}
}

// Refine — строки тултипа с подсказками; notes — что поменялось (в журнал).
func (h *Hints) Refine(ts []paddle.Text, chars []string) (lines []string, notes []string) {
	h.prepare(chars)
	var times []string
	for _, t := range ts {
		text := t.Text
		if len(t.Segs) == 1 && h.enc != nil {
			if name, ok := h.name(t.Segs[0]); ok && name != text {
				notes = append(notes, fmt.Sprintf("«%s» → %s", text, name))
				text = name
			}
		}
		lines = append(lines, text)
		if h.TimeRunes != "" && strings.ContainsFunc(t.Text, unicode.IsDigit) {
			for _, sg := range t.Segs {
				if tm := compactTime(paddle.DecodeAllowed(sg.Probs, sg.T, sg.C, chars, paddle.Allow(chars, h.TimeRunes))); tm != "" {
					times = append(times, tm)
				}
			}
		}
	}
	for _, tm := range times {
		lines = append(lines, HintTime+tm)
		notes = append(notes, "время "+HintTime+tm)
	}
	return lines, notes
}

// name — название зоны, если кусок строки похож на него заметно больше, чем
// на любое другое, и почти так же, как на то, что модель прочла сама.
func (h *Hints) name(sg paddle.Seg) (string, bool) {
	runes := utf8.RuneCountInString(sg.Text)
	if runes < 4 || strings.ContainsFunc(sg.Text, unicode.IsDigit) {
		return "", false
	}
	greedy := paddle.GreedyLogLik(sg.Probs, sg.T, sg.C)
	// Сначала дёшево: по расстоянию правки до прочитанного — 40 ближайших
	// названий; вероятность модели считаем только для них.
	type cand struct {
		name string
		lab  []int
		dist int
	}
	read := []rune(strings.ToLower(sg.Text))
	var cs []cand
	for n, lab := range h.enc {
		if 2*len(lab)+1 > 2*sg.T || abs(len(lab)-runes) > runes/2+2 {
			continue
		}
		cs = append(cs, cand{n, lab, zonecard.Levenshtein(read, []rune(strings.ToLower(n)))})
	}
	sort.Slice(cs, func(i, j int) bool {
		return cs[i].dist < cs[j].dist || cs[i].dist == cs[j].dist && cs[i].name < cs[j].name
	})
	if len(cs) > nameShortlist {
		cs = cs[:nameShortlist]
	}
	best, second := -1e18, -1e18
	bestName := ""
	for _, c := range cs {
		l := paddle.CTCLogLik(sg.Probs, sg.T, sg.C, c.lab)
		if l > best {
			best, second, bestName = l, best, c.name
		} else if l > second {
			second = l
		}
	}
	if bestName == "" || best < greedy-(nameSlack+nameSlackRune*float64(runes)) || best-second < nameMargin {
		return "", false
	}
	return bestName, true
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// reHM — «7ч05м», «7 h 5 m»; reMS — «49м27с». Последнее совпадение в строке:
// время стоит в конце строки «Закроется через …».
var (
	reHM = regexp.MustCompile(`(\d{1,2})\s*[чh]\s*(\d{1,2})\s*[мm]`)
	reMS = regexp.MustCompile(`(\d{1,2})\s*[мm]\s*(\d{1,2})\s*[сs]`)
)

// compactTime — время из строки, прочитанной только цифрами и единицами,
// в виде «7ч05м» / «49м27с»; "" — нет правдоподобного. Две цифры часов
// больше 23 — первая из них прилипла от соседнего слова («246ч26м» → 6ч26м).
func compactTime(s string) string {
	s = strings.ToLower(s)
	if ms := reHM.FindAllStringSubmatch(s, -1); len(ms) > 0 {
		m := ms[len(ms)-1]
		hh, _ := strconv.Atoi(m[1])
		mm, _ := strconv.Atoi(m[2])
		if hh > 23 && len(m[1]) == 2 {
			hh = hh % 10
		}
		if hh <= 23 && mm <= 59 && hh+mm > 0 {
			return fmt.Sprintf("%dч%02dм", hh, mm)
		}
	}
	if ms := reMS.FindAllStringSubmatch(s, -1); len(ms) > 0 {
		m := ms[len(ms)-1]
		mm, _ := strconv.Atoi(m[1])
		ss, _ := strconv.Atoi(m[2])
		if mm <= 59 && ss <= 59 && mm+ss > 0 {
			return fmt.Sprintf("%dм%02dс", mm, ss)
		}
	}
	return ""
}
