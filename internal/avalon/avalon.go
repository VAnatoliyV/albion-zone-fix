// Пакет avalon — карта Дорог Авалона: справочник зон, текущая зона, проходы
// A→B и их отправка на общий сервер карты. Правила те же, что у мак-версии
// (app/Карта.swift) и сервера (docs/2026-10-07-карта-авалона.md):
//
//   - первая зона после запуска — не проход: неизвестно, как человек туда попал;
//   - карта только для Европы: сервер игры не Европа — ничего не шлём;
//     неизвестный сервер ("") шлём, как мак;
//   - отправляем только пары из справочника, где хотя бы одна зона — дорога
//     (иначе сервер всё равно откажет, а лишние отчёты — лишний след).
package avalon

import (
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
)

// ServerURL — сервер карты (aj-live в Oracle). Отчёты — POST /roads/report.
const ServerURL = "https://129-80-180-213.sslip.io"

// SiteURL — сайт с разделом «Карта Авалона».
const SiteURL = "https://vanatoliyv.github.io/albion-craft-profit/"

// Zone — зона из справочника сайта и сервера (zones.json).
type Zone struct {
	Code string `json:"code"`
	Name string `json:"name"`
	Tier int    `json:"tier"`
	// Kind — тип зоны: roads, black, red, yellow, safe, city, island,
	// instance, other (поле q справочника).
	Kind string `json:"kind"`
	Road bool   `json:"road"` // дороги Авалона
}

// zones.json — копия albion-craft-profit/zones.json (тот же справочник, что
// у сайта и сервера карты). Обновлять копированием.
//
//go:embed zones.json
var zonesRaw []byte

var (
	dirOnce sync.Once
	dir     map[string]Zone
)

// Directory — справочник зон по коду.
func Directory() map[string]Zone {
	dirOnce.Do(func() {
		var f struct {
			Zones []struct {
				C string `json:"c"`
				N string `json:"n"`
				T int    `json:"t"`
				Q string `json:"q"`
				R bool   `json:"r"`
			} `json:"zones"`
		}
		dir = map[string]Zone{}
		if json.Unmarshal(zonesRaw, &f) != nil {
			return
		}
		for _, z := range f.Zones {
			dir[z.C] = Zone{Code: z.C, Name: z.N, Tier: z.T, Kind: z.Q, Road: z.R}
		}
	})
	return dir
}

// Lookup — зона по коду.
func Lookup(code string) (Zone, bool) {
	z, ok := Directory()[code]
	return z, ok
}

// Region — сервер игры по адресу игрового сервера ("ip:port"): "europe",
// "americas", "asia" или "" (неизвестно). Диапазоны — как GetServer у
// albiondata-client.
func Region(addr string) string {
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	switch {
	case strings.HasPrefix(host, "193.169.238."):
		return "europe"
	case strings.HasPrefix(host, "5.188.125."):
		return "americas"
	case strings.HasPrefix(host, "5.45.187."):
		return "asia"
	}
	return ""
}

// RegionOK — с этого сервера можно слать на карту: Европа или неизвестно.
func RegionOK(r string) bool { return r == "" || r == "europe" }

// Place — где игрок: код зоны и сервер игры.
type Place struct {
	Zone   string
	Region string
}

// Pass — проход из зоны в зону.
type Pass struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Region string `json:"region,omitempty"`
}

// Skip — почему смена зоны не стала отчётом.
type Skip int

const (
	OK          Skip = iota // проход, отправлять
	First                   // первая зона после запуска
	Same                    // та же зона (переподключение)
	NotEurope               // сервер игры не Европа
	MixedRegion             // зоны с разных серверов (сменил сервер)
	Unknown                 // зоны нет в справочнике
	NotRoad                 // ни одна из зон не дорога Авалона
)

// Decide решает, проход ли смена зоны prev → cur. prev == nil — это первая
// зона после запуска. Pass заполнен и тогда, когда отправлять нельзя:
// по нему вкладка показывает причину.
func Decide(prev *Place, cur Place) (Pass, Skip) {
	if prev == nil || prev.Zone == "" {
		return Pass{}, First
	}
	if prev.Zone == cur.Zone {
		return Pass{}, Same
	}
	p := Pass{From: prev.Zone, To: cur.Zone, Region: cur.Region}
	from, ok1 := Lookup(prev.Zone)
	to, ok2 := Lookup(cur.Zone)
	switch {
	case !ok1 || !ok2:
		return p, Unknown
	case !from.Road && !to.Road:
		return p, NotRoad
	case !RegionOK(cur.Region):
		return p, NotEurope
	case prev.Region != "" && cur.Region != "" && prev.Region != cur.Region:
		return p, MixedRegion
	}
	return p, OK
}

// Report — тело отчёта pass для POST /roads/report.
type Report struct {
	Kind    string `json:"kind"`
	Install string `json:"install"`
	From    string `json:"from"`
	To      string `json:"to"`
	Server  string `json:"server,omitempty"`
}

// Body — отчёт о проходе. Пустой сервер не пишем (как мак).
func Body(p Pass, install string) Report {
	return Report{Kind: "pass", Install: install, From: p.From, To: p.To, Server: p.Region}
}

// NewInstall — случайный номер установки (UUID v4). Ничего о человеке не
// говорит; нужен серверу для лимитов и бана.
func NewInstall() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// JSCode — код зоны, безопасный для строки JS в одинарных кавычках и для
// адреса: только латиница, цифры, @ и -.
func JSCode(code string) string {
	var sb strings.Builder
	for _, c := range code {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '@' || c == '-' {
			sb.WriteRune(c)
		}
	}
	return sb.String()
}

// MapURL — раздел карты на сайте с подсвеченной зоной.
func MapURL(code string) string {
	u := SiteURL + "#map"
	if c := JSCode(code); c != "" {
		u += "/here=" + c
	}
	return u
}

// HereJS — подсветить зону на открытой карте, не перезагружая. "" — нечего.
func HereJS(code string) string {
	c := JSCode(code)
	if c == "" {
		return ""
	}
	return "window.avalonHere && window.avalonHere('" + c + "')"
}
