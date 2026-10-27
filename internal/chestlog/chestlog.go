// Package chestlog сверяет лог лута с логом сундука гильдии: кто что поднял
// и чего из этого не положил в сундук.
//
// Лог сундука игрок копирует в игре (сундук → Лог → «Копировать») и вставляет
// в приложение. Формат — шесть колонок через табуляцию, каждая в кавычках,
// первой строкой заголовок на языке клиента:
//
//	"Date"	"Player"	"Item"	"Enchantment"	"Quality"	"Amount"
//
// Предмет записан названием на языке клиента, поэтому названия переводим в
// коды по таблице item_loc.json с сайта (собирается из файлов игры).
// Отрицательное количество — предмет забрали из сундука.
//
// Сравниваем суммы по игроку и предмету (как Loot Logger Viewer): поднял
// минус положил — не хватает. Качество не учитываем — в логе лута его нет.
package chestlog

import (
	"encoding/csv"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Row — одна строка лога сундука.
type Row struct {
	Date    time.Time
	HasDate bool
	Player  string
	Name    string
	Ench    int
	Amount  int64
}

// Parse разбирает вставленный лог. Разделитель — табуляция, на всякий случай
// понимаем и «;», и «,». Строки, где количество не число (заголовок), пропускаем.
func Parse(text string) []Row {
	text = strings.TrimPrefix(strings.ReplaceAll(text, "\r\n", "\n"), "\ufeff")
	var out []Row
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		f := splitLine(line)
		if len(f) < 6 {
			continue
		}
		amount, err := strconv.ParseInt(strings.TrimSpace(f[5]), 10, 64)
		if err != nil {
			continue
		}
		ench, _ := strconv.Atoi(strings.TrimSpace(f[3]))
		r := Row{Player: strings.TrimSpace(f[1]), Name: strings.TrimSpace(f[2]), Ench: ench, Amount: amount}
		r.Date, r.HasDate = parseDate(strings.TrimSpace(f[0]))
		out = append(out, r)
	}
	return out
}

func splitLine(line string) []string {
	for _, sep := range []rune{'\t', ';', ','} {
		if !strings.ContainsRune(line, sep) {
			continue
		}
		rd := csv.NewReader(strings.NewReader(line))
		rd.Comma = sep
		rd.LazyQuotes = true
		rd.FieldsPerRecord = -1
		if f, err := rd.Read(); err == nil && len(f) >= 6 {
			return f
		}
	}
	return nil
}

var dateLayouts = []string{
	"01/02/2006 15:04:05", "02/01/2006 15:04:05", "2006-01-02 15:04:05",
	"02.01.2006 15:04:05", "2.1.2006 15.04.05", "1/2/2006 3:04:05 PM", "01/02/2006 03:04:05 PM",
}

func parseDate(s string) (time.Time, bool) {
	for _, l := range dateLayouts {
		if t, err := time.ParseInLocation(l, s, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// Loot — запись лога лута (то, что пишет сборщик в файл сессии).
type Loot struct {
	T      int64  `json:"t"`
	By     string `json:"by"`
	Item   string `json:"item"`
	Name   string `json:"name"`
	N      int64  `json:"n"`
	Silver bool   `json:"silver"`
}

// Line — один предмет в отчёте.
type Line struct {
	Item string `json:"item"`
	Name string `json:"name"`
	N    int64  `json:"n"`
}

// Player — итог по одному игроку.
type Player struct {
	Name      string `json:"name"`
	Looted    int64  `json:"looted"`
	Deposited int64  `json:"deposited"`
	Missing   []Line `json:"missing"`
	MissingN  int64  `json:"missingN"`
}

// Report — итог сверки.
type Report struct {
	Rows      int      `json:"rows"`      // строк лога сундука разобрано
	Deposits  int      `json:"deposits"`  // из них положили (а не забрали)
	Old       int      `json:"old"`       // положено раньше, чем начался лог лута
	Unknown   []string `json:"unknown"`   // названия, которых нет в таблице
	Players   []Player `json:"players"`
	NoLoot    bool     `json:"noLoot"`
	NoPlayers []string `json:"noPlayers"` // клали в сундук, но в логе лута их нет
}

var levelRe = regexp.MustCompile(`_LEVEL\d+`)

// Key — предмет без учёта того, как игра записала зачарование: у ресурсов
// T4_FIBER_LEVEL1@1, у снаряжения T6_MAIN_SWORD@1. Ключ — основа и уровень
// зачарования: T4_FIBER@1, T6_MAIN_SWORD@1.
func Key(code string, ench int) string {
	base := code
	if i := strings.IndexByte(code, '@'); i >= 0 {
		base = code[:i]
		if ench < 0 {
			ench, _ = strconv.Atoi(code[i+1:])
		}
	}
	if ench < 0 {
		ench = 0
	}
	return levelRe.ReplaceAllString(base, "") + "@" + strconv.Itoa(ench)
}

// Reconcile сверяет лог лута с логом сундука.
func Reconcile(rows []Row, loot []Loot, names *Names) Report {
	rep := Report{Rows: len(rows)}
	type acc struct {
		name      string
		looted    map[string]int64
		deposited map[string]int64
		titles    map[string]string
		codes     map[string]string
	}
	players := map[string]*acc{}
	get := func(name string) *acc {
		k := strings.ToLower(name)
		a := players[k]
		if a == nil {
			a = &acc{name: name, looted: map[string]int64{}, deposited: map[string]int64{}, titles: map[string]string{}, codes: map[string]string{}}
			players[k] = a
		}
		return a
	}
	var first int64
	looterKeys := map[string]bool{}
	for _, l := range loot {
		if l.Silver || l.By == "" || l.N <= 0 || strings.HasPrefix(l.Item, "#") || l.Item == "" {
			continue
		}
		if first == 0 || l.T < first {
			first = l.T
		}
		a := get(l.By)
		k := Key(l.Item, -1)
		a.looted[k] += l.N
		a.codes[k] = l.Item
		if l.Name != "" {
			a.titles[k] = l.Name
		}
		looterKeys[k] = true
	}
	rep.NoLoot = len(players) == 0
	unknown := map[string]bool{}
	noPlayers := map[string]bool{}
	for _, r := range rows {
		if r.Amount <= 0 || r.Player == "" {
			continue
		}
		rep.Deposits++
		// Положено задолго до первой записи лута — к этой вылазке не относится.
		// Запас в 12 часов: часовой пояс лога в игре бывает не наш.
		if r.HasDate && first > 0 && r.Date.Unix() < first-12*3600 {
			rep.Old++
			continue
		}
		codes := names.Lookup(r.Name)
		if len(codes) == 0 {
			unknown[r.Name] = true
			continue
		}
		k := Key(codes[0], r.Ench)
		for _, c := range codes {
			if ck := Key(c, r.Ench); looterKeys[ck] {
				k = ck
				break
			}
		}
		if _, ok := players[strings.ToLower(r.Player)]; !ok {
			noPlayers[r.Player] = true
		}
		get(r.Player).deposited[k] += r.Amount
	}
	for _, a := range players {
		p := Player{Name: a.name}
		for k, n := range a.looted {
			p.Looted += n
			if miss := n - a.deposited[k]; miss > 0 {
				p.Missing = append(p.Missing, Line{Item: a.codes[k], Name: a.titles[k], N: miss})
				p.MissingN += miss
			}
		}
		for _, n := range a.deposited {
			p.Deposited += n
		}
		if p.Looted == 0 {
			continue // только клал — в списке «клали, но не лутали»
		}
		sort.Slice(p.Missing, func(i, j int) bool { return p.Missing[i].N > p.Missing[j].N })
		rep.Players = append(rep.Players, p)
	}
	sort.Slice(rep.Players, func(i, j int) bool {
		if rep.Players[i].MissingN != rep.Players[j].MissingN {
			return rep.Players[i].MissingN > rep.Players[j].MissingN
		}
		return rep.Players[i].Name < rep.Players[j].Name
	})
	for n := range unknown {
		rep.Unknown = append(rep.Unknown, n)
	}
	sort.Strings(rep.Unknown)
	for n := range noPlayers {
		rep.NoPlayers = append(rep.NoPlayers, n)
	}
	sort.Strings(rep.NoPlayers)
	return rep
}
