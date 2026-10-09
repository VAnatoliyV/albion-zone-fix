package zonecard

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Tooltip — что прочитано с тултипа портала дорог (ТултипПортала у мака).
type Tooltip struct {
	// Read — название зоны, как его прочитали с экрана. Ещё не опознано.
	Read string `json:"read"`
	// Size — размер портала: 7 из «7/7». 0 — не распознан (рядом значок, и
	// OCR иногда читает всю полоску как «+2 n/a»).
	Size int `json:"size,omitempty"`
	// Left — сколько порталу жить на момент снимка; 0 — не прочитано.
	Left time.Duration `json:"left,omitempty"`
	// TimeLoose — время прочитано только нестрого (tolerantTime): на
	// карточке показываем, на карту не отправляем.
	TimeLoose bool `json:"timeLoose,omitempty"`
	// Unstable — «Нестабильные Пути в …»: портал в один конец (из Мглы или
	// Брецилиена), время — «для вашей группы». На общую карту не идёт.
	Unstable bool `json:"unstable,omitempty"`
	// TitleLoose — заголовок опознан только нестрого (искажённый, или его нет
	// вовсе): нестабильный путь не исключить — на карту не идёт.
	TitleLoose bool `json:"titleLoose,omitempty"`
}

// Признак, что перед нами портал дорог, а не случайный текст, — markers
// (phrases.go): «Путь Авалона в», «Road of Avalon to», «Straße von Avalon
// nach»… и «Нестабильные Пути в …» — портал в один конец (время «для
// вашей группы») — на всех языках клиента игры.

// markerAt — номер признака в строке или -1. Мелкий серый заголовок Windows
// OCR читает с ошибками в буквах («Авапона», «Пvть», «Rood»), поэтому
// сравнение нестрогое: до 1 ошибки на короткий признак, до 2 — на длинный.
// Строку «Биом: Пути Авалона» (на любом языке) из тултипа нестабильного
// пути не берём.
func markerAt(line string) bool { return markerIn(line, false) }

// markerIn — markerAt, а если строгий не нашёлся — по искажённому тексту
// (garbledMarker). withTime — в снимке есть время.
func markerIn(line string, withTime bool) bool {
	if biome(strings.Join(cyrTokens(line), "")) {
		return false
	}
	return markerStrict(line) || garbledMarker(line, withTime)
}

func markerStrict(line string) bool {
	ll := strings.ToLower(line)
	if hasBiome(ll) {
		return false
	}
	if fullMarkerIn(ll) {
		return true
	}
	// «Roads of Avalon», «Straßen von Avalon» — значение биома без подписи
	// (подпись ушла в другую строку): на признак похоже, но не он.
	if likeNotMarker(line) {
		return false
	}
	for _, m := range markers {
		if strings.Contains(ll, m) {
			return true
		}
	}
	for _, m := range markers {
		k := 1
		if len([]rune(m)) >= 12 {
			k = 2
		}
		if fuzzyContains(ll, m, k) {
			return true
		}
	}
	return false
}

// fuzzyContains — есть ли в s подстрока, отличающаяся от pat не больше чем
// на k правок (вставка, удаление, замена букв). Алгоритм Селлерса.
func fuzzyContains(s, pat string, k int) bool {
	p, t := []rune(pat), []rune(s)
	if len(p) == 0 {
		return true
	}
	prev := make([]int, len(p)+1)
	cur := make([]int, len(p)+1)
	for i := range prev {
		prev[i] = i
	}
	if prev[len(p)] <= k {
		return true
	}
	for _, c := range t {
		cur[0] = 0
		for i := 1; i <= len(p); i++ {
			cost := 1
			if p[i-1] == c {
				cost = 0
			}
			cur[i] = min(prev[i-1]+cost, prev[i]+1, cur[i-1]+1)
		}
		if cur[len(p)] <= k {
			return true
		}
		prev, cur = cur, prev
	}
	return false
}

// HasMarker — в строках есть признак тултипа портала (по нему выбирается
// язык OCR, на котором тултип прочитан).
func HasMarker(lines []string) bool { return markerIndex(lines) >= 0 }

// markerIndex — строка признака: сначала строгий признак на любой строке,
// потом искажённый (garbledMarker; хвосту «Авалона» без предлога нужно
// строгое время в снимке). -1 — нет.
func markerIndex(lines []string) int {
	for i, l := range lines {
		if markerAt(l) {
			return i
		}
	}
	withTime := timeLeft(lines) > 0
	for i, l := range lines {
		if markerIn(l, withTime) {
			return i
		}
	}
	return -1
}

// ParseTooltip разбирает строки с экрана. false — это не тултип портала.
func ParseTooltip(lines []string) (Tooltip, bool) {
	var clean []string
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			clean = append(clean, l)
		}
	}
	idx := markerIndex(clean)
	if idx < 0 {
		return Tooltip{}, false
	}
	// Название — либо в той же строке после «to», либо в следующей.
	name := ""
	line := clean[idx]
	low := strings.ToLower(line)
	if len(low) != len(line) {
		line = low // редкие буквы меняют длину при ToLower: режем по нижнему регистру
	}
	// Название — после последнего предлога признака (« to », « в »,
	// « nach »; турецкое «çıkışı:» — с двоеточием, его считаем пробелом).
	// Если предлогов несколько — берём самый правый.
	low2 := strings.ReplaceAll(low, ":", " ") + " "
	best, bestLen := -1, 0
	for _, p := range prepositions {
		if k := strings.LastIndex(low2, p); k > best {
			best, bestLen = k, len(p)
		}
	}
	if best >= 0 && best+bestLen <= len(line) {
		// strings.ToLower не меняет длину у наших букв (латиница и
		// кириллица), поэтому индекс годится и для исходной строки.
		name = line[best+bestLen:]
	}
	after := idx + 1 // строка сразу после блока «признак + название»
	if len([]rune(Clean(name))) < 4 && idx+1 < len(clean) {
		// Строка времени — не название («6 q 17 N» после заголовка).
		if isTimeLine(clean[idx+1]) {
			return Tooltip{}, false
		}
		name = clean[idx+1]
		after = idx + 2
	}
	name = Clean(name)
	if len([]rune(name)) < 4 {
		return Tooltip{}, false
	}
	t := Tooltip{Read: name, Size: portalSize(clean), Left: timeLeft(clean), Unstable: unstableIn(clean, idx),
		TitleLoose: !markerStrict(clean[idx])}
	if t.Left == 0 {
		// В тултипе — и время искажённое («6 q 17 N»): только нестрого,
		// на карту не идёт.
		if t.Left = tolerantLeft(clean, after); t.Left > 0 {
			t.TimeLoose = true
		}
	}
	return t, true
}

// tolerantLeft — время по искажённым строкам (tolerantTime): только строка
// «Закроется через …» или строка after сразу после признака с названием.
func tolerantLeft(lines []string, after int) time.Duration {
	units := unitsFor(lines)
	for i, l := range lines {
		hint := strings.HasPrefix(l, HintTime)
		if i != after && !timeContext(l) && !hint {
			continue
		}
		if d := tolerantTimeU(strings.TrimPrefix(l, HintTime), units); d > 0 {
			return d
		}
	}
	return 0
}

// Clean убирает мусор распознавания: значок черепа, вопросительные знаки,
// кавычки — всё, что не буква, не цифра, не дефис, не апостроф и не пробел.
func Clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '\'' || r == ' ' {
			return r
		}
		return -1
	}, s)
	return strings.Trim(s, " ")
}

var reSize = regexp.MustCompile(`(\d{1,2})\s*/\s*(\d{1,2})`)

func portalSize(lines []string) int {
	for _, l := range lines {
		if m := reSize.FindStringSubmatch(l); m != nil {
			if n, err := strconv.Atoi(m[2]); err == nil {
				return n
			}
		}
	}
	return 0
}

// «5 h 47 m», «49 m 27 s», «23 м 14 с», «1 d 2 h». \D{0,3} — как у мака:
// между числом и следующим числом бывает «min», «ч.» и прочее.
var reTime = regexp.MustCompile(`(?:(\d{1,3})\s*[dд]\D{0,3})?(?:(\d{1,3})\s*[hчr]\D{0,3})?(?:(\d{1,3})\s*[mм]\D{0,3})?(?:(\d{1,3})\s*[sс])?`)

// reCompact — часы и минуты подряд, слепленные мелким шрифтом: «7ч05м»,
// «р7ч05м». Сами по себе — признак строки времени (в названиях зон цифр нет).
var reCompact = regexp.MustCompile(`\d{1,2}\s*[hч]\s*\d{1,2}\s*[mм]`)

// HintTime — пометка строки времени, найденной чтением только цифр и единиц
// (ocr.Hints): такое время — только нестрогое, на карту после подтверждения
// вторым снимком.
const HintTime = "≈ "

func timeLeft(lines []string) time.Duration {
	units := unitsFor(lines)
	for _, l := range lines {
		if strings.HasPrefix(l, HintTime) {
			continue
		}
		low := strings.ToLower(l)
		if reSize.MatchString(low) && !hasCloseWord(low) {
			continue // «7/7» — размер портала, не время
		}
		low = normUnits(low, units) // «5 sa 3 dk», «2 st 7 m» → «5 h 3 m», «2 h 7 m»
		if !(strings.Contains(low, "closes") || strings.Contains(low, "закро") || strings.Contains(low, "через") || strings.Contains(low, ":") ||
			strings.Contains(low, " m") || strings.Contains(low, " м") || hasCloseWord(low) || reCompact.MatchString(low)) {
			continue
		}
		for _, m := range reTime.FindAllStringSubmatch(low, -1) {
			part := func(i int) int64 {
				n, _ := strconv.ParseInt(m[i], 10, 64)
				return n
			}
			sec := part(1)*86400 + part(2)*3600 + part(3)*60 + part(4)
			if sec > 0 && time.Duration(sec)*time.Second <= MaxLeft {
				return time.Duration(sec) * time.Second
			}
		}
	}
	return 0
}
