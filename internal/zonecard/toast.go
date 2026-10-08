package zonecard

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"albionzonefix/internal/i18n"
)

// Текст уведомления — как enum Уведомление мак-версии (app/Оверлей.swift):
// заголовок — название зоны, подзаголовок — Оформление.подзаголовок(з, со:
// true), тело — строки(з, р) по переключателям.

// ToastOptions — что писать в уведомлении (переключатели настроек, по
// умолчанию всё включено и сундуки сверху — как у мака).
type ToastOptions struct {
	Chests      bool
	Res         bool
	Dungeons    bool
	Portal      bool
	ChestsFirst bool
}

// Toast — готовый текст уведомления.
type Toast struct {
	Title    string
	Subtitle string
	Body     string
}

// label — надпись словаря; ключа нет — пусто (виды дорог, которые словом не
// подписываем: «чёрная дорога» ничего не добавляет).
func label(lang, key string) string {
	if _, ok := i18n.Table[key]; !ok {
		return ""
	}
	return i18n.T(lang, key)
}

// Icon — цветной квадратик зоны: в уведомлении красить текст нельзя, а
// эмодзи система рисует цветными всегда.
func Icon(quality string) string {
	switch quality {
	case "safe":
		return "🟦"
	case "yellow":
		return "🟨"
	case "red":
		return "🟥"
	case "black":
		return "⬛️"
	case "city":
		return "🏰"
	case "island":
		return "🏝"
	case "mists":
		return "🌫"
	case "instance":
		return "🕳"
	case "roads":
		return "🌀"
	}
	return "▫️"
}

var roman = []string{"", "I", "II", "III", "IV", "V", "VI"}

// Kind — вид зоны: дороги — видом (дорога убежищ, королевская), обычные —
// качеством (синяя, жёлтая…). У дорог градация римской цифрой.
func Kind(lang string, z *Zone) string {
	var name string
	if z.Road {
		name = label(lang, "type."+z.Type)
	} else {
		name = i18n.T(lang, "q."+z.Quality)
	}
	if name == "" {
		return ""
	}
	if z.Road && z.Grade > 0 && z.Grade < len(roman) {
		return name + " " + roman[z.Grade]
	}
	return name
}

// Subtitle — «⬛️ чёрная зона · T6 · качество 2». withIcon — с квадратиком.
// У мака при пустом виде с квадратиком остаётся «🌀 » с пробелом; здесь
// пробел обрезан (иначе двойной пробел перед «·»).
func Subtitle(lang string, z *Zone, withIcon bool) string {
	var parts []string
	head := Kind(lang, z)
	if withIcon {
		head = Icon(z.Quality) + " " + head
	}
	if head = strings.TrimRight(head, " "); strings.TrimSpace(head) != "" {
		parts = append(parts, head)
	}
	if z.Tier > 0 {
		parts = append(parts, fmt.Sprintf("T%d", z.Tier))
	}
	if !z.Road && z.Grade > 0 {
		parts = append(parts, i18n.Tf(lang, "q.grade", z.Grade))
	}
	return strings.Join(parts, " · ")
}

// chestSquare — редкость сундука цветным квадратиком: зелёный обычный,
// синий ветеранский, фиолетовый и жёлтый элитные.
var chestSquare = map[string]string{
	"small": "🟩", "small_veteran": "🟦", "medium_veteran": "🟦",
	"small_elite": "🟪", "medium_elite": "🟨", "large_elite": "🟨",
}

// ChestSquare — квадратик сундука ("📦" — неизвестный вид).
func ChestSquare(kind string) string {
	if s, ok := chestSquare[kind]; ok {
		return s
	}
	return "📦"
}

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// ChestsLine — «🟩 1 × малый   🟦 2 × средний ветеранский»; "" — сундуков нет.
func ChestsLine(lang string, z *Zone) string {
	if len(z.Chests) == 0 {
		return ""
	}
	var out []string
	for _, k := range sortedKeys(z.Chests) {
		out = append(out, fmt.Sprintf("%s %d × %s", ChestSquare(k), z.Chests[k], i18n.T(lang, "chest."+k)))
	}
	return strings.Join(out, "   ")
}

// ResLine — ресурсы без тиров (баннер узкий): «волокно · шкуры»; в открытом
// мире — по биому, основной со звёздочкой. "" — ресурсов нет.
func ResLine(lang string, z *Zone) string {
	var out []string
	if len(z.Biome) == 0 {
		for _, k := range sortedKeys(z.Res) {
			out = append(out, i18n.T(lang, "res."+k))
		}
	} else {
		for i, k := range z.Biome {
			s := i18n.T(lang, "res."+k)
			if i == 0 {
				s = "★ " + s
			}
			out = append(out, s)
		}
	}
	return strings.Join(out, " · ")
}

// Clock — «5 ч 46 м» или «49:27» (Время.строкой у мака).
func Clock(lang string, d time.Duration) string {
	all := int(d / time.Second)
	h, m, s := all/3600, (all%3600)/60, all%60
	if h > 0 {
		return i18n.Tf(lang, "t.hm", h, m)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// approx — «≈» перед временем, прочитанным только нестрого.
func approx(r Result) string {
	if r.Tooltip.TimeLoose {
		return "≈"
	}
	return ""
}

// BuildToast — текст уведомления для зоны z и снимка r на момент now.
func BuildToast(lang string, z *Zone, r Result, o ToastOptions, now time.Time) Toast {
	var chests, res, other []string
	if o.Chests {
		if s := ChestsLine(lang, z); s != "" {
			chests = append(chests, s)
		}
	}
	if o.Res {
		if s := ResLine(lang, z); s != "" {
			res = append(res, s)
		}
	}
	if o.Dungeons && len(z.Dungeons) > 0 {
		var ds []string
		for _, k := range sortedKeys(z.Dungeons) {
			ds = append(ds, fmt.Sprintf("%d × %s", z.Dungeons[k], i18n.T(lang, "dng."+k)))
		}
		other = append(other, i18n.T(lang, "zn.dungeons")+": "+strings.Join(ds, ", "))
	}
	if o.Portal {
		var tail []string
		if r.Tooltip.Size > 0 {
			tail = append(tail, i18n.Tf(lang, "zn.portal", r.Tooltip.Size))
		}
		if left, ok := r.LeftAt(now); ok {
			tail = append(tail, i18n.Tf(lang, "zn.closes", approx(r)+Clock(lang, left)))
		}
		if len(tail) > 0 {
			other = append(other, strings.Join(tail, " · "))
		}
	}
	first, second := chests, res
	if !o.ChestsFirst {
		first, second = res, chests
	}
	lines := make([]string, 0, len(chests)+len(res)+len(other))
	lines = append(lines, first...)
	lines = append(lines, second...)
	lines = append(lines, other...)
	title := z.Name
	if len(r.Matches) > 0 && r.Doubtful() {
		title += " ?" // сомнительно — с вопросом, как на панели
	}
	return Toast{Title: title, Subtitle: Subtitle(lang, z, true), Body: strings.Join(lines, "\n")}
}
