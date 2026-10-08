package zonecard

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"albionzonefix/internal/i18n"
	"albionzonefix/internal/settings"
)

// Как показать готовую карточку кроме вкладки «Зона» — как СпособПоказа у
// мака (zoneShow): уведомлением Windows, панелью поверх игры или никак. И в
// уведомлении, и на панели — только те части, что включены переключателями
// (сундуки, ресурсы, данжи, портал), в выбранном порядке.

// Presentation — что показать: nil — не показывать.
type Presentation struct {
	Toast *Toast
	Panel *Panel
}

// Options — переключатели частей карточки из настроек.
func Options(s settings.Settings) ToastOptions {
	return ToastOptions{Chests: s.NotifyChests, Res: s.NotifyRes, Dungeons: s.NotifyDng, Portal: s.NotifyPortal,
		ChestsFirst: s.NotifyOrder != "resourcesFirst"}
}

// Present решает, что показать по снимку sh при настройках s на момент
// now. Снимок не вышел или зона не узнана — ничего (ошибка видна во
// вкладке). panelOK=false — панель в этом запуске сломалась или не
// создалась: вместо неё уведомление Windows.
func Present(lang string, sh Shot, s settings.Settings, now time.Time, panelOK bool) Presentation {
	z := sh.Result.Zone()
	if sh.Kind == ErrKindBlank {
		// Пустой снимок — подсказка тем же способом, что и карточка: во
		// вкладке её не видно, пока человек в игре.
		switch settings.NormalizeShow(s.ZoneShow) {
		case settings.ShowPanel:
			if panelOK {
				return Presentation{Panel: &Panel{Title: i18n.T(lang, "zn.blankTitle"), Footer: i18n.T(lang, "zn.blankShort"), Warn: true}}
			}
			fallthrough
		case settings.ShowNotify:
			return Presentation{Toast: &Toast{Title: i18n.T(lang, "zn.blankTitle"), Body: i18n.T(lang, "zn.blank")}}
		}
		return Presentation{}
	}
	if sh.Kind != "" || z == nil {
		return Presentation{}
	}
	o := Options(s)
	switch settings.NormalizeShow(s.ZoneShow) {
	case settings.ShowPanel:
		if panelOK {
			p := BuildPanel(lang, z, sh.Result, o, now)
			return Presentation{Panel: &p}
		}
		fallthrough
	case settings.ShowNotify:
		t := BuildToast(lang, z, sh.Result, o, now)
		return Presentation{Toast: &t}
	}
	return Presentation{}
}

// Panel — сжатая карточка для панели поверх игры (КарточкаЗоны(сжато:) у
// мака): название, вид и тир, строки частей, внизу портал. Цвета — 0xRRGGBB.
type Panel struct {
	Title    string
	Color    uint32 // квадратик цвета зоны
	Subtitle string
	Doubt    string // «похоже на эту, рядом …»; "" — уверенно
	Rows     []PanelRow
	Footer   string
	Warn     bool // портал закроется меньше чем через 5 минут — подсветить
}

// PanelRow — подпись слева и значки справа (переносятся по ширине).
type PanelRow struct {
	Label string
	Items []PanelItem
}

// PanelItem — цветной квадратик (Color 0 — без него) и текст; Main —
// основной ресурс биома (золотом).
type PanelItem struct {
	Color uint32
	Text  string
	Main  bool
}

// QualityColor — цвет зоны, как QCOLOR страницы.
var QualityColor = map[string]uint32{
	"safe": 0x4E77C4, "yellow": 0xD8B23A, "red": 0xC24A3E, "black": 0x6B6F7A,
	"city": 0x8A6A3A, "island": 0x4E8E5A, "roads": 0x8A5ABF,
}

// ChestColor — редкость сундука: зелёный обычный, синий ветеранский,
// фиолетовый и жёлтый элитные (те же, что квадратики уведомления).
var ChestColor = map[string]uint32{
	"small": 0x4FAF5A, "small_veteran": 0x4E77C4, "medium_veteran": 0x4E77C4,
	"small_elite": 0x8A5ABF, "medium_elite": 0xD8B23A, "large_elite": 0xD8B23A,
}

// ResColor — вид ресурса: значков предметов на панели нет, есть цвет.
var ResColor = map[string]uint32{
	"FIBER": 0x9DBF4A, "HIDE": 0xB9774A, "ORE": 0x8C96A8, "ROCK": 0xB5AFA4, "WOOD": 0x9A7046,
}

const mutedColor = 0x8B93A8

// warnLeft — сколько до закрытия портала считается «скоро».
const warnLeft = 5 * time.Minute

// BuildPanel — сжатая карточка зоны z по снимку r с частями o на момент now.
func BuildPanel(lang string, z *Zone, r Result, o ToastOptions, now time.Time) Panel {
	p := Panel{Title: z.Name, Color: QualityColor[z.Quality], Subtitle: Subtitle(lang, z, false)}
	if p.Color == 0 {
		p.Color = mutedColor
	}
	if r.Doubtful() && len(r.Matches) > 1 {
		p.Doubt = i18n.Tf(lang, "zn.doubt", r.Matches[1].Zone.Name)
	}
	var chests, res, other []PanelRow
	if o.Res {
		if row, ok := resRow(lang, z); ok {
			res = append(res, row)
		}
	}
	if o.Chests && (len(z.Chests) > 0 || z.Road) {
		row := PanelRow{Label: i18n.T(lang, "zn.camps")}
		for _, k := range sortedKeys(z.Chests) {
			c, ok := ChestColor[k]
			if !ok {
				c = mutedColor
			}
			row.Items = append(row.Items, PanelItem{Color: c, Text: fmt.Sprintf("%d × %s", z.Chests[k], i18n.T(lang, "chest."+k))})
		}
		if len(row.Items) == 0 {
			row.Items = []PanelItem{{Text: i18n.T(lang, "zn.none")}}
		}
		chests = append(chests, row)
	}
	if o.Dungeons && (len(z.Dungeons) > 0 || z.Mists) {
		row := PanelRow{Label: i18n.T(lang, "zn.dungeons")}
		for _, k := range sortedKeys(z.Dungeons) {
			row.Items = append(row.Items, PanelItem{Text: fmt.Sprintf("%d × %s", z.Dungeons[k], i18n.T(lang, "dng."+k))})
		}
		if z.Mists {
			row.Items = append(row.Items, PanelItem{Text: i18n.T(lang, "zn.mists")})
		}
		other = append(other, row)
	}
	if o.ChestsFirst {
		p.Rows = append(append(chests, res...), other...)
	} else {
		p.Rows = append(append(res, chests...), other...)
	}
	if o.Portal {
		var tail []string
		if r.Tooltip.Size > 0 {
			tail = append(tail, i18n.Tf(lang, "zn.portal", r.Tooltip.Size))
		}
		if left, ok := r.LeftAt(now); ok {
			tail = append(tail, i18n.Tf(lang, "zn.closes", Clock(lang, left)))
			p.Warn = left < warnLeft
		}
		p.Footer = strings.Join(tail, " · ")
	}
	return p
}

// resRow — ресурсы: в открытом мире по биому (основной первым, золотом),
// у дорог — виды с разбросом тиров и числом узлов.
func resRow(lang string, z *Zone) (PanelRow, bool) {
	if len(z.Biome) > 0 {
		row := PanelRow{Label: i18n.T(lang, "zn.biome")}
		for i, k := range z.Biome {
			row.Items = append(row.Items, PanelItem{Color: ResColor[k], Text: i18n.T(lang, "res."+k), Main: i == 0})
		}
		return row, true
	}
	if len(z.Res) == 0 {
		return PanelRow{}, false
	}
	label := "zn.biome"
	if z.Road {
		label = "zn.nodes"
	}
	row := PanelRow{Label: i18n.T(lang, label)}
	for _, k := range sortedKeys(z.Res) {
		var tiers []int
		n := 0
		for _, pair := range z.Res[k] {
			if len(pair) == 2 {
				tiers = append(tiers, pair[0])
				n += pair[1]
			}
		}
		text := i18n.T(lang, "res."+k)
		if len(tiers) > 0 {
			sort.Ints(tiers)
			tr := fmt.Sprintf("T%d", tiers[len(tiers)-1])
			if tiers[0] != tiers[len(tiers)-1] {
				tr = fmt.Sprintf("T%d–T%d", tiers[0], tiers[len(tiers)-1])
			}
			text += " " + tr
			if n > 0 {
				text += fmt.Sprintf(" ×%d", n)
			}
		}
		row.Items = append(row.Items, PanelItem{Color: ResColor[k], Text: text})
	}
	return row, true
}
