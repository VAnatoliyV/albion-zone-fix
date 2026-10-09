package zonecard

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Тултип портала прямо в игре (не на карте мира) — мелкий текст поверх
// сцены, и Windows OCR читает его сильно искажённым: «ABan0H0 a» вместо
// «Авалона в» (английский движок на русском тексте), «—лутьАволоно в»,
// «6 q 17 N» вместо «6 ч 17 м». Здесь — нормализация таких строк: признак
// тултипа, время и нестрогое название.

// toCyr — латиница и цифры, которыми английский движок OCR пишет русский
// заголовок, → кириллица. Регистр важен: B — «В», b — «ь»; e — «в» (в
// игровом шрифте «в» похожа на «e»: «AeanoH0» = «Авалона»).
var toCyr = map[rune]rune{
	'A': 'а', 'a': 'а', 'B': 'в', 'b': 'ь', 'C': 'с', 'c': 'с', 'E': 'е', 'e': 'в',
	'H': 'н', 'h': 'н', 'K': 'к', 'k': 'к', 'M': 'м', 'm': 'м', 'N': 'п', 'n': 'п',
	'O': 'о', 'o': 'о', '0': 'о', 'P': 'р', 'p': 'р', 'T': 'т', 't': 'т', 'X': 'х', 'x': 'х',
	'Y': 'у', 'y': 'у', 'u': 'и', 'q': 'ч', '4': 'ч', 'l': 'л', 'I': 'л', '1': 'л', '|': 'л',
}

// canonCyr — частые путаницы мелкого шрифта: «о» и «а», «л» и «п» —
// одна буква (сравнение после этой замены).
var canonCyr = map[rune]rune{'о': 'а', 'л': 'п', 'ё': 'е'}

// cyrTokens — строка как русский текст: двойники латиницы → кириллица,
// нижний регистр, о/а и л/п сведены; слова — по пробелам и знакам.
func cyrTokens(s string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for _, r := range s {
		if c, ok := toCyr[r]; ok {
			r = c
		}
		if !unicode.IsLetter(r) {
			flush()
			continue
		}
		r = unicode.ToLower(r)
		if c, ok := canonCyr[r]; ok {
			r = c
		}
		b.WriteRune(r)
	}
	flush()
	return out
}

// toLat — кириллица-двойники и цифры → латиница (для английского
// заголовка и названий зон). Шире homoglyphs: «Сгееп» = «Green».
var toLat = map[rune]rune{
	'а': 'a', 'в': 'b', 'е': 'e', 'ё': 'e', 'к': 'k', 'м': 'm', 'н': 'h', 'о': 'o', 'р': 'p',
	'с': 'c', 'т': 't', 'у': 'y', 'х': 'x', 'і': 'i', 'ј': 'j', 'ѕ': 's', 'г': 'r', 'п': 'n',
	'ь': 'b', 'и': 'u', 'л': 'n', 'д': 'd', 'з': 'z',
	'0': 'o', '1': 'l', '|': 'l',
}

// latKey — строка как латиница без пробелов и знаков, нижний регистр.
// Заглавная I посреди слова — почти всегда «l» («GreenhoUwVaIe»).
func latKey(s string) string {
	var b strings.Builder
	prevLetter := false
	for _, r := range s {
		if r == 'I' && prevLetter {
			r = 'l'
		}
		if r == 'П' && prevLetter {
			// «П» посреди слова — две палочки «ll» («ГреепЬоПмК1е»).
			b.WriteString("ll")
			continue
		}
		lr := unicode.ToLower(r)
		if c, ok := toLat[lr]; ok {
			lr = c
		}
		prevLetter = unicode.IsLetter(r)
		if lr < 128 && unicode.IsLetter(lr) {
			b.WriteRune(lr)
		}
	}
	return b.String()
}

// Признак в нормализованном виде (canonCyr уже применён: «авалона» →
// «авапана»).
var (
	cyrMarkers = []string{"путьавапана", "нестабипьныепути", "дарагаавапана"}
	// Латинские признаки всех языков — latMarkers (phrases.go).
	// avalonTail — хвост «Авалона» без «Путь»: «ABan0H0 a» — заголовок, у
	// которого «Путь» не прочитался вовсе.
	avalonTail = "авапана"
)

// garbledMarker — признак тултипа в искажённой строке: нестрогое вхождение
// всего признака (до четверти длины ошибок) или «Авалона» + предлог «в»
// последним словом. time — в снимке есть время (тогда хватает и хвоста
// «Авалона» без предлога, но строже).
func garbledMarker(line string, time bool) bool {
	toks := cyrTokens(line)
	if len(toks) == 0 {
		return false
	}
	joined := strings.Join(toks, "")
	if notPortal(joined, latKey(line)) {
		return false
	}
	// «Биом: Пути Авалона» (тултип зоны дорог и нестабильного пути) — не
	// признак, даже если «Биом» ушёл в другую строку: «Пути», а не «Путь».
	if biome(joined) {
		return false
	}
	for _, m := range cyrMarkers {
		if fuzzyContains(joined, m, len([]rune(m))/4) {
			return true
		}
	}
	if lat := latKey(line); !strings.Contains(lat, "biom") && !strings.Contains(lat, "biyom") && !likeNotMarker(line) {
		for _, m := range latMarkers {
			if fuzzyContains(lat, m, len(m)/4) {
				return true
			}
		}
	}
	// «Авалона в» последним словом: предлог «в» английский движок пишет
	// «B», «a» или «e».
	// Слово должно кончаться на «Авалона» («…ьАвалона», «Жалоно»), а не
	// «Аваланш», «Авалонский».
	last := toks[len(toks)-1]
	if len(toks) >= 2 && (last == "в" || last == "а") {
		if endsLike(toks[len(toks)-2], avalonTail, 2) {
			return true
		}
	}
	// С временем в снимке хватает и «Авалона» отдельным словом (последним
	// или предпоследним) — с точностью до одной буквы, без суффикса.
	if time {
		for i := max(0, len(toks)-2); i < len(toks); i++ {
			if Levenshtein([]rune(toks[i]), []rune(avalonTail)) <= 1 {
				return true
			}
		}
	}
	return false
}

// notPortal — фразы с «Авалоном», которые не портал: сундуки, стражи,
// авалонские мобы и предметы.
func notPortal(cyrJoined, lat string) bool {
	for _, w := range []string{"сундук", "страж", "кпюч"} {
		if strings.Contains(cyrJoined, w) {
			return true
		}
	}
	for _, w := range []string{"chest", "guard", "avalonian", "key"} {
		if strings.Contains(lat, w) {
			return true
		}
	}
	return false
}

// endsLike — слово tok кончается на что-то в пределах k правок от pat, и
// последняя буква та же («…авапана», а не «…авапанш»).
func endsLike(tok, pat string, k int) bool {
	t, p := []rune(tok), []rune(pat)
	if len(t) == 0 || t[len(t)-1] != p[len(p)-1] {
		return false
	}
	for l := len(p) - k; l <= len(p)+k; l++ {
		if l <= 0 || l > len(t) {
			continue
		}
		if Levenshtein(t[len(t)-l:], p) <= k {
			return true
		}
	}
	return false
}

// biome — строка из таблицы тултипа «Биом … Пути Авалона».
func biome(joined string) bool {
	return strings.Contains(joined, "биам") || strings.HasSuffix(joined, "путиавапана")
}

// WeakMarker — в строках есть хотя бы обрывок признака: слово, которое
// начинается на «авал»/«aval», или «путь» вплотную к «авал»: тултип,
// скорее всего, под курсором, только прочитан плохо — стоит дочитать
// вариантами картинки, а не переснимать. «провал», «карнавал», «путь
// домой» — не обрывок.
func WeakMarker(lines []string) bool {
	for _, l := range lines {
		toks := cyrTokens(l)
		joined := strings.Join(toks, "")
		if biome(joined) || notPortal(joined, latKey(l)) {
			continue
		}
		for _, t := range toks {
			if strings.HasPrefix(t, "авап") || strings.Contains(t, "путьав") {
				return true
			}
		}
		for _, w := range strings.FieldsFunc(strings.ToLower(l), func(r rune) bool { return !unicode.IsLetter(r) }) {
			if strings.HasPrefix(w, "aval") || strings.HasPrefix(w, "awal") {
				return true
			}
		}
		if strings.Contains(latKey(l), "roadofav") {
			return true
		}
	}
	return false
}

// --- время ---

// reTok — числа и всё остальное по отдельности.
var reTok = regexp.MustCompile(`\d+|[^\d\s]+`)

// Единицы времени, как их путает OCR: «q», «Ч», «4» — «ч»; «N», «Н», «M» —
// «м»; «C» — «с».
func unitOf(tok string) byte {
	switch strings.ToLower(strings.Trim(tok, ".,:;")) {
	case "ч", "q", "h", "r", "чч":
		return 'h'
	case "м", "m", "n", "н", "min", "мин", "nn":
		return 'm'
	case "с", "c", "s", "сек", "sec":
		return 's'
	case "д", "d":
		return 'd'
	}
	// Единицы других языков клиента: «st», «sa», «dk», «sn», «t», «g», «j».
	if u, ok := unitWords[strings.ToLower(strings.Trim(tok, ".,:;"))]; ok {
		return u
	}
	return 0
}

// junk — короткий не-буквенный мусор между числами («"», «.», «•»): там
// стояла единица, которую OCR не узнал.
func junk(tok string) bool {
	if len([]rune(tok)) > 2 {
		return false
	}
	for _, r := range tok {
		if unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

type timePart struct {
	n    int
	unit byte // 'd', 'h', 'm', 's'; 0 — нет; '?' — непонятный мусор
}

// tolerantTime — время из сильно искажённой строки. Только явный случай:
// числа с единицами по убыванию («6 q 17 N» — 6 ч 17 м), или два числа, из
// которых у второго единица, а между ними мусор («15 "40M» — 15 ч 40 м:
// игра пишет две единицы, и раз вторая — минуты, первая — часы). Лишнее
// число без единицы («2 5 26 C») или одни минуты без часов и секунд («39 M»
// — скорее всего, обрезанное «X ч 39 м») — неоднозначно, 0.
func tolerantTime(line string) time.Duration {
	toks := reTok.FindAllString(line, -1)
	// «б» — шестёрка в игровом шрифте (только отдельным словом).
	for i, t := range toks {
		if t == "б" || t == "Б" {
			toks[i] = "6"
		}
	}
	// Строка размера портала («7/7 N», «20/20 Н») — не время.
	if reSize.MatchString(line) {
		return 0
	}
	// «6 4 17 M»: одиночная «4» между числами — это «ч», но только в строке
	// «Закроется через …» (иначе «3 4 5 M» стало бы 3 ч 5 м).
	for i := 1; timeContext(line) && i+2 < len(toks); i++ {
		if toks[i] == "4" && isNum(toks[i-1]) && isNum(toks[i+1]) && unitOf(toks[i+2]) == 'm' {
			toks[i] = "ч"
		}
	}
	// Последняя группа «число [единица]» подряд.
	var groups [][]timePart
	var cur []timePart
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if !isNum(t) {
			if len(cur) > 0 {
				groups = append(groups, cur)
				cur = nil
			}
			continue
		}
		n, _ := strconv.Atoi(t)
		p := timePart{n: n}
		if i+1 < len(toks) && !isNum(toks[i+1]) {
			if u := unitOf(toks[i+1]); u != 0 {
				p.unit = u
				i++
			} else if junk(toks[i+1]) {
				p.unit = '?'
				i++
			} else {
				cur = append(cur, p)
				groups = append(groups, cur)
				cur = nil
				i++
				continue
			}
		}
		cur = append(cur, p)
	}
	if len(cur) > 0 {
		groups = append(groups, cur)
	}
	for g := len(groups) - 1; g >= 0; g-- {
		if d := groupTime(groups[g]); d > 0 {
			return d
		}
	}
	return 0
}

// timeContext — строка «Закроется через …» / «Closes in …» на любом языке
// клиента (или значок песочных часов перед временем).
func timeContext(line string) bool {
	low := strings.ToLower(line)
	for _, w := range []string{"закро", "через", "closes", "close", "⏳", "⌛", "ⴟ"} {
		if strings.Contains(low, w) {
			return true
		}
	}
	return hasCloseWord(low)
}

// isTimeLine — строка — это время (строгое или искажённое), а не название.
func isTimeLine(line string) bool {
	return timeLeft([]string{line}) > 0 || tolerantTime(line) > 0
}

func isNum(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func groupTime(ps []timePart) time.Duration {
	// Хвост группы, в котором последнее число с явной единицей.
	for len(ps) > 0 && (ps[len(ps)-1].unit == 0 || ps[len(ps)-1].unit == '?') {
		ps = ps[:len(ps)-1]
	}
	if len(ps) == 0 {
		return 0
	}
	// «15 "40M»: два числа, у первого мусор вместо единицы.
	if len(ps) == 2 && ps[0].unit == '?' {
		switch ps[1].unit {
		case 'm':
			ps[0].unit = 'h'
		case 's':
			ps[0].unit = 'm'
		}
	}
	order := map[byte]int{'d': 0, 'h': 1, 'm': 2, 's': 3}
	var sec int64
	prev := -1
	has := map[byte]bool{}
	for _, p := range ps {
		o, ok := order[p.unit]
		if !ok || o <= prev {
			return 0 // число без единицы или единицы не по убыванию
		}
		if (p.unit == 'm' || p.unit == 's') && p.n >= 60 || p.unit == 'h' && p.n >= 48 {
			return 0
		}
		prev = o
		has[p.unit] = true
		mult := map[byte]int64{'d': 86400, 'h': 3600, 'm': 60, 's': 1}[p.unit]
		sec += int64(p.n) * mult
	}
	if has['m'] && !has['h'] && !has['s'] && !has['d'] {
		return 0 // одни минуты — скорее обрезанное «X ч NN м»
	}
	if has['s'] && !has['m'] && !has['h'] && !has['d'] {
		return 0 // одни секунды — скорее обрезанное «X м NN с»
	}
	return time.Duration(sec) * time.Second
}

// --- нестрогое название ---

// LooseThreshold — с таким сходством название берётся при найденном
// признаке тултипа, если строго не опозналось, и только с отрывом
// LooseMargin от следующей зоны с другим названием. Итог — сомнительный.
const (
	LooseThreshold = 0.55
	LooseMargin    = 0.05
)

// looseOK — нестрогое совпадение годится: выше порога и с отрывом.
func looseOK(m []Match) bool {
	if len(m) == 0 || m[0].Closeness < LooseThreshold {
		return false
	}
	for _, x := range m[1:] {
		if x.Zone.Name == m[0].Zone.Name {
			continue
		}
		return m[0].Closeness-x.Closeness >= LooseMargin
	}
	return true
}

// NameFloor — ниже этого сходства название не узнано вовсе (UnknownError),
// а не «похоже на …»: «0ИТ-Еготшт» → Ouyos-Aoeuam (0.08) карточкой быть не
// должно.
const NameFloor = 0.5

// confusable — буквы, которые мелкий шрифт путает: замена дешевле.
var confusable = map[[2]rune]bool{}

func init() {
	for _, p := range []string{"il", "ce", "ca", "ao", "oe", "nh", "bh", "uv", "uw", "vw", "rt", "mn", "ft", "ij", "lt", "ou", "kl", "sz", "ec", "un", "rn", "cg", "mw", "kv"} {
		r := []rune(p)
		confusable[[2]rune{r[0], r[1]}] = true
		confusable[[2]rune{r[1], r[0]}] = true
	}
}

// looseDistance — Левенштейн, где замена похожих букв стоит 0.4.
func looseDistance(a, b []rune) float64 {
	prev := make([]float64, len(b)+1)
	cur := make([]float64, len(b)+1)
	for j := range prev {
		prev[j] = float64(j)
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = float64(i)
		for j := 1; j <= len(b); j++ {
			cost := 1.0
			switch {
			case a[i-1] == b[j-1]:
				cost = 0
			case confusable[[2]rune{a[i-1], b[j-1]}]:
				cost = 0.4
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// SimilarLoose — до n зон, похожих на text с поправкой на путаницы
// шрифта (без пробелов и дефисов, кириллица-двойники → латиница).
func (d *Dict) SimilarLoose(text string, n int) []Match {
	needle := []rune(latKey(text))
	if len(needle) < 5 {
		return nil
	}
	out := make([]Match, 0, len(d.Zones))
	for i := range d.Zones {
		z := &d.Zones[i]
		hay := []rune(latKey(z.Name))
		dist := looseDistance(needle, hay)
		out = append(out, Match{Zone: z, Closeness: 1 - dist/float64(max(len(needle), len(hay)))})
	}
	sortMatches(out)
	if len(out) > n {
		out = out[:n]
	}
	return out
}
