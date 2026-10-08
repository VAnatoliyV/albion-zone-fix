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
}

// markers — признак, что перед нами портал дорог, а не случайный текст.
// Русский клиент пишет «Путь Авалона в», английский — «Road of Avalon to».
var markers = []string{"road of avalon", "путь авалона", "дорога авалона",
	// «Нестабильные Пути в …» — портал в один конец (время «для вашей группы»).
	"нестабильные пути", "unstable roads", "unstable road"}

// markerAt — номер признака в строке или -1. Мелкий серый заголовок Windows
// OCR читает с ошибками в буквах («Авапона», «Пvть», «Rood»), поэтому
// сравнение нестрогое: до 1 ошибки на короткий признак, до 2 — на длинный.
// Строку «Биом: Пути Авалона» из тултипа нестабильного пути не берём.
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
	if strings.Contains(ll, "биом") || strings.Contains(ll, "biome") {
		return false
	}
	for _, m := range markers {
		if strings.Contains(ll, m) {
			return true
		}
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

// prepositions — после них в той же строке может стоять название.
var prepositions = []string{" to ", " в "}

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
	for _, p := range prepositions {
		if k := strings.LastIndex(low, p); k >= 0 {
			// strings.ToLower не меняет длину у наших букв (латиница и
			// кириллица), поэтому индекс годится и для исходной строки.
			name = line[k+len(p):]
			break
		}
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
	t := Tooltip{Read: name, Size: portalSize(clean), Left: timeLeft(clean)}
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
	for i, l := range lines {
		if i != after && !timeContext(l) {
			continue
		}
		if d := tolerantTime(l); d > 0 {
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

func timeLeft(lines []string) time.Duration {
	for _, l := range lines {
		low := strings.ToLower(l)
		if !(strings.Contains(low, "closes") || strings.Contains(low, "закро") || strings.Contains(low, "через") || strings.Contains(low, ":") ||
			strings.Contains(low, " m") || strings.Contains(low, " м")) {
			continue
		}
		for _, m := range reTime.FindAllStringSubmatch(low, -1) {
			part := func(i int) int64 {
				n, _ := strconv.ParseInt(m[i], 10, 64)
				return n
			}
			sec := part(1)*86400 + part(2)*3600 + part(3)*60 + part(4)
			if sec > 0 {
				return time.Duration(sec) * time.Second
			}
		}
	}
	return 0
}
