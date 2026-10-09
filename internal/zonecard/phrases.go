package zonecard

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// Фразы тултипа портала дорог на всех языках клиента игры — из данных игры
// (ao-bin-dumps, localization.xml), общий файл мака и Windows
// ../tooltip-phrases.json. Здесь — копия (tools/phrases/sync.sh),
// встроенная в exe. Ключи строк: ROADS_OF_AVALON_TOOLTIP_TITLE (признак),
// PROXIMITY_TOOLTIP_MISTS_CITY_EXIT_SUB_TITLE (нестабильный путь),
// ROADS_OF_AVALON_TOOLTIP_CLOSE_TIME и *_CLOSES_IN (время),
// GENERIC_TIME_*_SHORT (единицы), PROXIMITY_TOOLTIP_EXIT_BIOME(_ROADS) —
// «Биом: Пути Авалона», не признак.
//
//go:embed tooltip-phrases.json
var phrasesJSON []byte

type langPhrases struct {
	Marker    []string            `json:"marker"`
	Unstable  []string            `json:"unstable"`
	Closes    []string            `json:"closes"`
	Party     []string            `json:"party"`
	Biome     []string            `json:"biome"`
	NotMarker []string            `json:"notMarker"`
	Units     map[string][]string `json:"units"`
}

// skipScript — языки, которых наши модели OCR не читают (арабица,
// иероглифы, хангыль): их фразы только мешали бы нестрогому сравнению.
var skipScript = map[string]bool{"ar": true, "ja": true, "ko": true, "zh-cn": true, "zh-tw": true}

// skipUnits — единицы, которые спорят с остальными языками: у индонезийского
// «h» — дни, «j» — часы, «d» — секунды.
var skipUnits = map[string]bool{"id": true}

var (
	// markers — признак тултипа в нижнем регистре: заголовок и нестабильный
	// путь всех языков без последнего слова-предлога («road of avalon»,
	// «straße von avalon»): предлог OCR часто теряет или уносит к названию.
	markers []string
	// latMarkers — те же признаки как latKey (для искажённых строк).
	latMarkers []string
	// prepositions — последнее слово признака: после него в той же строке
	// может стоять название (« to », « в », « nach », « çıkışı »…).
	prepositions []string
	// biomeWords — подпись «Биом»: строка с ней — не признак.
	biomeWords []string
	// notMarkers — значение «Пути Авалона» из той же строки биома: строка,
	// которая целиком похожа на него, — не признак (оно почти как признак:
	// «Roads of Avalon», «Straßen von Avalon»).
	notMarkers, notMarkersLat []string
	// closeWords — слова строки времени («закроется», «closes», «schließt»…).
	closeWords []string
	// unstableMarkers — признаки нестабильного пути (портал в один конец)
	// в нижнем регистре без предлога; partyPhrases — «Закроется для вашей
	// группы через» без подстановки: время у такого портала — групповое.
	unstableMarkers, partyPhrases []string
	// unitWords — единица времени языка → 'd', 'h', 'm', 's'.
	unitWords map[string]byte
	// reUnit — число и слово после него (единица на любом языке).
	reUnit = regexp.MustCompile(`(\d)\s*(\pL+)`)
)

// Старые написания, которых нет в данных игры (раньше были у клиента или
// у OCR): «Дорога Авалона», «Unstable Road».
var legacyMarkers = []string{"дорога авалона", "unstable road"}

func init() { loadPhrases(phrasesJSON) }

func loadPhrases(data []byte) {
	var all map[string]langPhrases
	if err := json.Unmarshal(data, &all); err != nil {
		panic("zonecard: tooltip-phrases.json: " + err.Error())
	}
	langs := make([]string, 0, len(all))
	for l := range all {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	mk, lat, pre, bio, nm, nml, cw := set{}, set{}, set{}, set{}, set{}, set{}, set{}
	un, party := set{}, set{}
	un.add("unstable road")
	units := map[string]byte{}
	for _, m := range legacyMarkers {
		mk.add(m)
	}
	for _, l := range langs {
		if skipScript[l] {
			continue
		}
		p := all[l]
		for _, s := range p.Unstable {
			if words := strings.Fields(strings.ToLower(strings.Trim(s, " :："))); len(words) >= 2 {
				un.add(strings.Join(words[:len(words)-1], " "))
			} else if len(words) == 1 {
				un.add(words[0])
			}
		}
		for _, s := range p.Party {
			party.add(strings.ToLower(s))
		}
		for _, s := range append(append([]string(nil), p.Marker...), p.Unstable...) {
			words := strings.Fields(strings.ToLower(strings.Trim(s, " :：")))
			if len(words) == 0 {
				continue
			}
			stem := words
			if len(words) >= 2 {
				stem = words[:len(words)-1]
				pre.add(" " + words[len(words)-1] + " ")
			}
			m := strings.Join(stem, " ")
			mk.add(m)
			if k := latKey(m); len(k) >= 10 {
				lat.add(k)
			}
		}
		for _, s := range p.Biome {
			bio.add(strings.ToLower(s))
		}
		for _, s := range p.NotMarker {
			nm.add(strings.ToLower(s))
			nml.add(latKey(s))
		}
		for _, s := range append(append([]string(nil), p.Closes...), p.Party...) {
			for _, w := range strings.Fields(strings.ToLower(s)) {
				if len([]rune(w)) >= 4 {
					cw.add(w)
					break
				}
			}
		}
		if !skipUnits[l] {
			for u, ws := range p.Units {
				if len(u) != 1 {
					continue
				}
				for _, w := range ws {
					units[strings.ToLower(w)] = u[0]
				}
			}
		}
	}
	markers, latMarkers, prepositions = mk.list, lat.list, pre.list
	biomeWords, notMarkers, notMarkersLat, closeWords = bio.list, nm.list, nml.list, cw.list
	unitWords = units
	unstableMarkers, partyPhrases = un.list, party.list
}

// unstableIn — портал в один конец: строка признака — «Нестабильные Пути
// в …» (нестрого, как markerStrict; искажённая русская — по cyrTokens), или
// в тултипе строка времени «для вашей группы».
func unstableIn(lines []string, idx int) bool {
	if idx >= 0 && idx < len(lines) {
		ll := strings.ToLower(lines[idx])
		for _, m := range unstableMarkers {
			k := 1
			if len([]rune(m)) >= 12 {
				k = 2
			}
			if strings.Contains(ll, m) || fuzzyContains(ll, m, k) {
				return true
			}
		}
		if fuzzyContains(strings.Join(cyrTokens(lines[idx]), ""), "нестабипьныепути", 4) {
			return true
		}
	}
	for _, l := range lines {
		ll := strings.ToLower(l)
		for _, p := range partyPhrases {
			if fuzzyContains(ll, p, len([]rune(p))/5) {
				return true
			}
		}
	}
	return false
}

// set — список без повторов в порядке добавления.
type set struct {
	seen map[string]bool
	list []string
}

func (s *set) add(v string) {
	if v == "" || s.seen[v] {
		return
	}
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	s.seen[v] = true
	s.list = append(s.list, v)
}

// hasBiome — в строке подпись «Биом» какого-нибудь языка.
func hasBiome(low string) bool {
	for _, b := range biomeWords {
		if strings.Contains(low, b) {
			return true
		}
	}
	return false
}

// likeNotMarker — строка целиком похожа на «Пути Авалона» (значение биома)
// какого-нибудь языка: до одной ошибки на всю строку.
func likeNotMarker(line string) bool {
	low := strings.ToLower(Clean(line))
	for _, n := range notMarkers {
		if Levenshtein([]rune(low), []rune(n)) <= 1 {
			return true
		}
	}
	lat := latKey(line)
	for _, n := range notMarkersLat {
		if len(n) >= 8 && Levenshtein([]rune(lat), []rune(n)) <= 1 {
			return true
		}
	}
	return false
}

// normUnits — единицы времени всех языков → d/h/m/s сразу после числа:
// «5 Std. 3 m» → «5 h. 3 m», «5 sa 3 dk» → «5 h 3 m». Строка — в нижнем
// регистре. Русские и английские единицы и незнакомые слова («q», «min»)
// не трогаем — их понимают reTime и unitOf, как раньше.
func normUnits(low string) string {
	return reUnit.ReplaceAllStringFunc(low, func(m string) string {
		sub := reUnit.FindStringSubmatch(m)
		if w := []rune(sub[2]); len(w) == 1 && strings.ContainsRune("dhmsдчмс", w[0]) {
			return m // русские и английские — как раньше (reTime их знает)
		}
		if u, ok := unitWords[sub[2]]; ok {
			return sub[1] + " " + string(rune(u))
		}
		return m
	})
}

// hasCloseWord — в строке слово строки времени какого-нибудь языка.
func hasCloseWord(low string) bool {
	for _, w := range closeWords {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}
