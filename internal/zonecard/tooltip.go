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
}

// markers — признак, что перед нами портал дорог, а не случайный текст.
// Русский клиент пишет «Путь Авалона в», английский — «Road of Avalon to».
var markers = []string{"road of avalon", "путь авалона", "дорога авалона"}

// prepositions — после них в той же строке может стоять название.
var prepositions = []string{" to ", " в "}

// HasMarker — в строках есть признак тултипа портала (по нему выбирается
// язык OCR, на котором тултип прочитан).
func HasMarker(lines []string) bool {
	for _, l := range lines {
		ll := strings.ToLower(l)
		for _, m := range markers {
			if strings.Contains(ll, m) {
				return true
			}
		}
	}
	return false
}

// ParseTooltip разбирает строки с экрана. false — это не тултип портала.
func ParseTooltip(lines []string) (Tooltip, bool) {
	var clean []string
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			clean = append(clean, l)
		}
	}
	idx := -1
	for i, l := range clean {
		ll := strings.ToLower(l)
		for _, m := range markers {
			if strings.Contains(ll, m) {
				idx = i
				break
			}
		}
		if idx >= 0 {
			break
		}
	}
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
	if len([]rune(Clean(name))) < 4 && idx+1 < len(clean) {
		name = clean[idx+1]
	}
	name = Clean(name)
	if len([]rune(name)) < 4 {
		return Tooltip{}, false
	}
	return Tooltip{Read: name, Size: portalSize(clean), Left: timeLeft(clean)}, true
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
		if !(strings.Contains(low, "closes") || strings.Contains(low, "закро") || strings.Contains(low, ":") ||
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
