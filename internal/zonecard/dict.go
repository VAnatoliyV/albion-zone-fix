// Пакет zonecard — карточка зоны по кнопке: разбор тултипа портала,
// нечёткое опознание зоны в справочнике, решение об отчёте на карту и риск
// чёрного экрана по истории переходов. Порт app/Зона.swift и
// app/Карта.swift мак-версии; проверки — на тех же настоящих строках с
// экрана (app/проверка-зоны.swift → dict_test.go, tooltip_test.go).
//
// Здесь нет ничего от Windows: снимок экрана, OCR, кнопка и уведомления —
// в internal/screen, internal/hotkey и internal/notify.
package zonecard

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"unicode"

	"albionzonefix/internal/avalon"
)

// Zone — зона справочника со всем, что показывает карточка. Поля — как у
// ЗонаДорог мак-версии; JSON — для страницы.
type Zone struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Tier    int    `json:"tier"`
	Type    string `json:"type"`    // тип из дампа: TUNNEL_HIDEOUT, OPENPVP_BLACK_1…
	Quality string `json:"quality"` // safe, yellow, red, black, city, island, roads, mists…
	Grade   int    `json:"grade"`   // градация внутри качества (0 — нет)
	Road    bool   `json:"road"`
	// Res — вид ресурса → [[тир, сколько узлов], …].
	Res map[string][][]int `json:"res"`
	// Biome — три ресурса биома, первый основной (у дорог пусто).
	Biome    []string       `json:"biome"`
	Chests   map[string]int `json:"chests"`
	Camps    map[string]int `json:"camps"`
	Dungeons map[string]int `json:"dungeons"`
	// Points — ресурсные точки на карте зоны (FIBER, HIDE, ORE, WOOD, ROCK).
	Points []string `json:"points"`
	Mists  bool     `json:"mists"` // есть выход в город туманов
}

// Dict — справочник зон.
type Dict struct {
	Zones  []Zone
	byName map[string]*Zone
	byCode map[string]*Zone
}

// NewDict строит справочник из готового списка (для тестов).
func NewDict(zs []Zone) *Dict {
	d := &Dict{Zones: zs, byName: map[string]*Zone{}, byCode: map[string]*Zone{}}
	for i := range d.Zones {
		z := &d.Zones[i]
		k := strings.ToLower(z.Name)
		if _, ok := d.byName[k]; !ok {
			d.byName[k] = z
		}
		d.byCode[z.Code] = z
	}
	return d
}

// Parse читает zones.json (поля c,n,t,q,r — их же читает сервер карты;
// остальное — для карточки, tools/собрать-зоны-для-карты.py).
func Parse(raw []byte) (*Dict, error) {
	var f struct {
		Zones []struct {
			C  string             `json:"c"`
			N  string             `json:"n"`
			T  int                `json:"t"`
			Q  string             `json:"q"`
			R  bool               `json:"r"`
			Re map[string][][]int `json:"res"`
			Ch map[string]int     `json:"ch"`
			Dg map[string]int     `json:"dg"`
			Rb []string           `json:"rb"`
			Ty string             `json:"ty"`
			G  int                `json:"g"`
			Lg map[string]int     `json:"lg"`
			Mi bool               `json:"mi"`
			Pt []string           `json:"pt"`
		} `json:"zones"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	zs := make([]Zone, 0, len(f.Zones))
	for _, z := range f.Zones {
		zs = append(zs, Zone{Code: z.C, Name: z.N, Tier: z.T, Quality: z.Q, Road: z.R, Res: z.Re,
			Chests: z.Ch, Dungeons: z.Dg, Biome: z.Rb, Type: z.Ty, Grade: z.G, Camps: z.Lg, Mists: z.Mi, Points: z.Pt})
	}
	return NewDict(zs), nil
}

var (
	defOnce sync.Once
	def     *Dict
)

// Default — справочник, зашитый в программу (тот же, что у сайта и сервера
// карты). nil — не прочитался.
func Default() *Dict {
	defOnce.Do(func() { def, _ = Parse(avalon.ZonesJSON()) })
	return def
}

// Exact — зона по точному названию (без учёта регистра).
func (d *Dict) Exact(name string) *Zone { return d.byName[strings.ToLower(strings.TrimSpace(name))] }

// ByCode — зона по коду.
func (d *Dict) ByCode(code string) *Zone { return d.byCode[code] }

// Match — кандидат опознания: 1.0 — точное совпадение.
type Match struct {
	Zone      *Zone   `json:"-"`
	Closeness float64 `json:"closeness"`
}

// Similar — до n самых похожих зон. Названия зон нарочно похожи (рядом
// Secent-Al-Qinsom, Secent-Qi-Qinsom и Setent-Al-Qinsum), поэтому отдаём
// несколько кандидатов, чтобы честно показать сомнение, а не угадывать молча.
func (d *Dict) Similar(text string, n int) []Match {
	needle := []rune(strings.ToLower(FoldLatin(strings.TrimSpace(text))))
	if len(needle) < 4 {
		return nil
	}
	out := make([]Match, 0, len(d.Zones))
	for i := range d.Zones {
		z := &d.Zones[i]
		hay := []rune(strings.ToLower(z.Name))
		dist := Levenshtein(needle, hay)
		out = append(out, Match{Zone: z, Closeness: 1 - float64(dist)/float64(max(len(needle), len(hay)))})
	}
	// Устойчиво: при равной близости — порядок справочника (как sorted у Swift
	// на практике для наших данных; важен только первый и второй).
	sort.SliceStable(out, func(i, j int) bool { return out[i].Closeness > out[j].Closeness })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// Levenshtein — сколько букв надо поправить.
func Levenshtein(a, b []rune) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// homoglyphs — кириллица, которую русский движок OCR Windows ставит вместо
// похожей латиницы. Названия зон в справочнике — латиницей у всех клиентов
// (русский тоже пишет «Cebos-Avemlum»), поэтому перед сравнением сводим.
var homoglyphs = map[rune]rune{
	'а': 'a', 'в': 'b', 'е': 'e', 'ё': 'e', 'к': 'k', 'м': 'm', 'н': 'h', 'о': 'o', 'р': 'p',
	'с': 'c', 'т': 't', 'у': 'y', 'х': 'x', 'і': 'i', 'ј': 'j', 'ѕ': 's',
	'А': 'A', 'В': 'B', 'Е': 'E', 'Ё': 'E', 'К': 'K', 'М': 'M', 'Н': 'H', 'О': 'O', 'Р': 'P',
	'С': 'C', 'Т': 'T', 'У': 'Y', 'Х': 'X', 'І': 'I', 'Ј': 'J', 'Ѕ': 'S',
}

// FoldLatin сводит похожую кириллицу к латинице — только если в строке
// латиница уже есть или вся строка из «двойников»: настоящее русское слово
// (вроде «Закроется») не трогаем, опознание ищет только латинские названия.
func FoldLatin(s string) string {
	latin, other := false, false
	for _, r := range s {
		switch {
		case r < 128 && unicode.IsLetter(r):
			latin = true
		case unicode.Is(unicode.Cyrillic, r):
			if _, ok := homoglyphs[r]; !ok {
				other = true
			}
		}
	}
	if other && !latin {
		return s
	}
	return strings.Map(func(r rune) rune {
		if l, ok := homoglyphs[r]; ok {
			return l
		}
		return r
	}, s)
}
