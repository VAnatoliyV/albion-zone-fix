package zonecard

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"
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
	// markersFull — признаки целиком, с предлогом («road of avalon to»):
	// строка с ним — признак, даже если похожа на значение биома.
	markersFull []string
	// closeWords — слова строки времени («закроется», «closes», «schließt»…).
	closeWords []string
	// unstableMarkers — признаки нестабильного пути (портал в один конец)
	// в нижнем регистре без предлога; partyPhrases — «Закроется для вашей
	// группы через» без подстановки: время у такого портала — групповое.
	unstableMarkers, partyPhrases []string
	// unstableKeys, partyKeys — то же как foldKey (без диакритики).
	unstableKeys, partyKeys []string
	// unitsByLang — единицы времени языка (кроме русских и английских d/h/m/s,
	// д/ч/м/с, их знает reTime) → 'd', 'h', 'm', 's'. Применяются только в
	// тултипе этого языка (unitsFor): «g» у турецкого и итальянского — дни, а в
	// русском тултипе «g» — испорченная «ч».
	unitsByLang map[string]map[string]byte
	// langSignals — фразы языка (заголовок, нестабильный путь, время) как
	// foldKey: по ним видно, что тултип на этом языке.
	langSignals map[string][]string
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
	un, party, full := set{}, set{}, set{}
	un.add("unstable road")
	byLang, signals := map[string]map[string]byte{}, map[string][]string{}
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
			full.add(strings.Join(words, " "))
			stem := words
			if len(words) >= 2 {
				stem = words[:len(words)-1]
				pre.add(" " + words[len(words)-1] + " ")
			}
			m := strings.Join(stem, " ")
			mk.add(m)
			if k := foldKey(m); len(k) >= 10 {
				lat.add(k)
			}
		}
		for _, s := range p.Biome {
			bio.add(strings.ToLower(s))
		}
		for _, s := range p.NotMarker {
			nm.add(strings.ToLower(s))
			nml.add(foldKey(s))
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
			units := map[string]byte{}
			for u, ws := range p.Units {
				if len(u) != 1 {
					continue
				}
				for _, w := range ws {
					w = strings.ToLower(w)
					if r := []rune(w); len(r) == 1 && strings.ContainsRune("dhmsдчмс", r[0]) {
						continue // русские и английские — как раньше (reTime)
					}
					units[w] = u[0]
				}
			}
			if len(units) > 0 {
				byLang[l] = units
				for _, s := range append(append(append(append([]string(nil), p.Closes...), p.Party...), p.Marker...), p.Unstable...) {
					if k := foldKey(s); len(k) >= 6 {
						signals[l] = append(signals[l], k)
					}
				}
			}
		}
	}
	biomeWords, notMarkers, notMarkersLat, closeWords = bio.list, nm.list, nml.list, cw.list
	markersFull, prepositions = full.list, pre.list
	// Признак, который совпадает со значением биома какого-нибудь языка
	// (индонезийское «Jalan Avalon» без «menuju»), — не признак: такой
	// стебель не берём, только фразу целиком.
	biomeKey := map[string]bool{}
	for _, n := range nml.list {
		biomeKey[n] = true
	}
	markers, latMarkers = nil, nil
	for _, m := range mk.list {
		if !biomeKey[foldKey(m)] {
			markers = append(markers, m)
		}
	}
	for _, m := range lat.list {
		if !biomeKey[m] {
			latMarkers = append(latMarkers, m)
		}
	}
	unitsByLang, langSignals = byLang, signals
	unstableMarkers, partyPhrases = un.list, party.list
	unstableKeys, partyKeys = nil, nil
	for _, m := range un.list {
		if k := foldKey(m); len(k) >= 8 {
			unstableKeys = append(unstableKeys, k)
		}
	}
	for _, p := range party.list {
		if k := foldKey(p); len(k) >= 8 {
			partyKeys = append(partyKeys, k)
		}
	}
}

// unstableIn — портал в один конец: в любой строке тултипа заголовок
// «Нестабильные Пути в …» или строка времени «для вашей группы» — на
// любом языке и не строже, чем ищется сам признак (garbledMarker: до
// четверти длины ошибок без диакритики). Ошибиться в сторону «нестабильный»
// безопасно: такой портал просто не идёт на карту. idx — строка признака
// (не нужна: смотрим все строки).
func unstableIn(lines []string, idx int) bool {
	_ = idx
	for _, l := range lines {
		if unstableLine(l) {
			return true
		}
	}
	return false
}

// UnstableSign — в строках (любого языка OCR, варианта, повтора) есть
// признак нестабильного пути.
func UnstableSign(lines []string) bool { return unstableIn(lines, -1) }

func unstableLine(l string) bool {
	ll := strings.ToLower(l)
	for _, m := range unstableMarkers {
		k := 1
		if len([]rune(m)) >= 12 {
			k = 2
		}
		if strings.Contains(ll, m) || fuzzyContains(ll, m, k) {
			return true
		}
	}
	key := foldKey(l)
	for _, m := range unstableKeys {
		if fuzzyContains(key, m, len(m)/4) {
			return true
		}
	}
	if fuzzyContains(strings.Join(cyrTokens(l), ""), "нестабипьныепути", 4) {
		return true
	}
	for _, p := range partyPhrases {
		if fuzzyContains(ll, p, len([]rune(p))/5) {
			return true
		}
	}
	for _, p := range partyKeys {
		if fuzzyContains(key, p, len(p)/4) {
			return true
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
// какого-нибудь языка. Кириллица — до одной ошибки; латиница — без
// диакритики (eslav теряет ś, ż, ß: «Sciezki Awalonu», «Stralben von
// Avalon») и до шестой части длины ошибок.
func likeNotMarker(line string) bool {
	low := strings.ToLower(Clean(line))
	for _, n := range notMarkers {
		if Levenshtein([]rune(low), []rune(n)) <= 1 {
			return true
		}
	}
	return likeNotMarkerKey(foldKey(line))
}

// likeNotMarkerKey — то же для готового foldKey.
func likeNotMarkerKey(k string) bool {
	for _, n := range notMarkersLat {
		if len(n) >= 8 && Levenshtein([]rune(k), []rune(n)) <= max(1, len(n)/6) {
			return true
		}
	}
	return false
}

// fullMarkerIn — в строке признак целиком, с предлогом.
func fullMarkerIn(low string) bool {
	for _, m := range markersFull {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}

// diacritics — буквы с диакритикой → латиница без неё (как их теряет OCR).
var diacritics = map[rune]string{
	'ß': "ss", 'ä': "a", 'ö': "o", 'ü': "u", 'à': "a", 'á': "a", 'â': "a", 'ã': "a", 'å': "a", 'ą': "a",
	'ç': "c", 'ć': "c", 'č': "c", 'è': "e", 'é': "e", 'ê': "e", 'ë': "e", 'ę': "e", 'ğ': "g",
	'ì': "i", 'í': "i", 'î': "i", 'ï': "i", 'ı': "i", 'ł': "l", 'ñ': "n", 'ń': "n",
	'ò': "o", 'ó': "o", 'ô': "o", 'õ': "o", 'ś': "s", 'ş': "s", 'š': "s", 'ù': "u", 'ú': "u", 'û': "u",
	'ý': "y", 'ź': "z", 'ż': "z", 'ž': "z",
}

// foldKey — latKey без диакритики: «Ścieżki Awalonu» → «sciezkiawalonu».
func foldKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if f, ok := diacritics[r]; ok {
			b.WriteString(f)
			continue
		}
		b.WriteRune(r)
	}
	return latKey(b.String())
}

// unitsFor — единицы языков, чьи фразы есть в строках (нестрого, без
// диакритики: eslav читает «Schließt» как «Schlielt»).
func unitsFor(lines []string) map[string]byte {
	var keys []string
	for _, l := range lines {
		keys = append(keys, foldKey(l))
	}
	out := map[string]byte{}
	for lang, sigs := range langSignals {
		found := false
		for _, s := range sigs {
			for _, k := range keys {
				if fuzzyContains(k, s, len(s)/5) {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if found {
			for w, u := range unitsByLang[lang] {
				out[w] = u
			}
		}
	}
	return out
}

// MaxLeft — дольше портал дорог не живёт: больше — ошибка чтения, не время.
const MaxLeft = 24 * time.Hour

// normUnits — единицы времени языков units → d/h/m/s сразу после числа:
// «5 sa 3 dk» → «5 h 3 m». Строка — в нижнем регистре. Русские и
// английские единицы и незнакомые слова («q», «min») не трогаем — их
// понимают reTime и unitOf, как раньше.
func normUnits(low string, units map[string]byte) string {
	return reUnit.ReplaceAllStringFunc(low, func(m string) string {
		sub := reUnit.FindStringSubmatch(m)
		if u, ok := units[sub[2]]; ok {
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
