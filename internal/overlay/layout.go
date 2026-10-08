// Пакет overlay — панель карточки зоны поверх игры (ОверлейЗоны у мака):
// маленькое окно без рамки, всегда сверху, не забирает фокус и пропускает
// клики в игру; в выбранном углу экрана с игрой на несколько секунд.
//
// Здесь — то, что не зависит от Windows: где поставить панель и как
// разложить сжатую карточку (zonecard.Panel) по строкам. Рисует GDI в
// panel_windows.go; на других системах панели нет.
//
// Видна в оконном режиме игры и в «окне без рамки». В эксклюзивном
// полноэкранном режиме Windows отдаёт экран игре целиком — панели не видно.
package overlay

import (
	"albionzonefix/internal/settings"
	"albionzonefix/internal/zonecard"
)

// Rect — прямоугольник в точках экрана (правая и нижняя границы не входят).
type Rect struct{ Left, Top, Right, Bottom int }

// Scale — логические точки (при 100 %) в настоящие пиксели для dpi.
func Scale(v, dpi int) int {
	if dpi <= 0 {
		dpi = 96
	}
	return (v*dpi + 48) / 96
}

// Margin — отступ панели от краёв экрана (логические точки), как у мака.
const Margin = 24

// Place — левый верхний угол панели w×h в углу corner рабочей области work
// (экран без панели задач) монитора с dpi. Панель больше экрана —
// прижимается к левому/верхнему краю.
func Place(work Rect, w, h int, corner string, dpi int) (x, y int) {
	m := Scale(Margin, dpi)
	left, top := work.Left+m, work.Top+m
	right, bottom := work.Right-m-w, work.Bottom-m-h
	switch settings.NormalizeCorner(corner) {
	case settings.CornerTopLeft:
		x, y = left, top
	case settings.CornerBottomLeft:
		x, y = left, bottom
	case settings.CornerBottomRight:
		x, y = right, bottom
	default:
		x, y = right, top
	}
	x = max(min(x, work.Right-w), work.Left)
	y = max(min(y, work.Bottom-h), work.Top)
	return x, y
}

// Font — какой шрифт у куска текста.
type Font int

const (
	FontTitle Font = iota // название зоны
	FontSmall             // вид и тир, подпись строки, низ
	FontText              // значки строк
)

// FontPx — кегль шрифта в логических точках.
var FontPx = map[Font]int{FontTitle: 15, FontSmall: 11, FontText: 12}

// Цвета панели (0xRRGGBB) — как :root страницы.
const (
	ColorBg    = 0x151924
	ColorLine  = 0x333C55
	ColorText  = 0xDDE2EC
	ColorMuted = 0x8B93A8
	ColorGold  = 0xEEBC4E
)

// Op — что нарисовать: Text — строку (обрезая многоточием по W), иначе —
// залить прямоугольник цветом.
type Op struct {
	X, Y, W, H int
	Color      uint32
	Font       Font
	Text       string
	IsText     bool
}

// Layout — разложенная панель: размер в пикселях и что рисовать.
type Layout struct {
	W, H   int
	Radius int
	DPI    int
	Ops    []Op
}

// Measure — ширина строки s шрифтом f в пикселях (при текущем dpi).
type Measure func(f Font, s string) int

// Размеры в логических точках.
const (
	width     = 300 // ширина панели
	pad       = 12  // поля
	labelW    = 62  // колонка подписей
	gap       = 8   // между подписью и значками, между значками
	square    = 9   // цветной квадратик
	titleH    = 21
	smallH    = 16
	lineH     = 19 // строка значков
	rowGap    = 5  // между строками
	radius    = 8
	sqTextGap = 5 // квадратик → текст
)

// Arrange раскладывает карточку p для dpi; measure мерит строки.
func Arrange(p zonecard.Panel, dpi int, measure Measure) Layout {
	s := func(v int) int { return Scale(v, dpi) }
	l := Layout{W: s(width), Radius: s(radius), DPI: dpi}
	x0, inner := s(pad), s(width-2*pad)
	y := s(pad)
	text := func(x, y, w, h int, c uint32, f Font, t string) {
		l.Ops = append(l.Ops, Op{X: x, Y: y, W: max(w, 0), H: h, Color: c, Font: f, Text: t, IsText: true})
	}
	fill := func(x, y, w, h int, c uint32) { l.Ops = append(l.Ops, Op{X: x, Y: y, W: w, H: h, Color: c}) }

	text(x0, y, inner, s(titleH), ColorGold, FontTitle, p.Title)
	y += s(titleH)
	sq := s(square)
	fill(x0, y+(s(smallH)-sq)/2, sq, sq, p.Color)
	text(x0+sq+s(sqTextGap), y, inner-sq-s(sqTextGap), s(smallH), ColorMuted, FontSmall, p.Subtitle)
	y += s(smallH)
	if p.Doubt != "" {
		text(x0, y, inner, s(smallH), ColorGold, FontSmall, p.Doubt)
		y += s(smallH)
	}

	if len(p.Rows) > 0 {
		y += s(rowGap)
	}
	itemsX := x0 + s(labelW) + s(gap)
	itemsW := x0 + inner - itemsX
	for i, row := range p.Rows {
		y += s(rowGap)
		if i > 0 { // разделитель между строками
			fill(x0, y, inner, max(1, s(1)), ColorLine)
		}
		y += s(rowGap)
		text(x0, y, s(labelW), s(lineH), ColorMuted, FontSmall, row.Label)
		x, lines := itemsX, 1
		for _, it := range row.Items {
			tw := measure(FontText, it.Text)
			w := tw
			if it.Color != 0 {
				w += sq + s(sqTextGap)
			}
			if x > itemsX && x+w > itemsX+itemsW { // не влезло — на следующую строку
				x, lines = itemsX, lines+1
			}
			ly := y + (lines-1)*s(lineH)
			tx := x
			if it.Color != 0 {
				fill(x, ly+(s(lineH)-sq)/2, sq, sq, it.Color)
				tx += sq + s(sqTextGap)
			}
			c := uint32(ColorText)
			if it.Main {
				c = ColorGold
			}
			text(tx, ly, min(tw, itemsX+itemsW-tx), s(lineH), c, FontText, it.Text)
			x += w + s(gap)
		}
		y += lines * s(lineH)
	}

	if p.Footer != "" {
		y += s(rowGap) * 2
		c := uint32(ColorMuted)
		if p.Warn {
			c = ColorGold
		}
		text(x0, y, inner, s(smallH), c, FontSmall, p.Footer)
		y += s(smallH)
	}
	l.H = y + s(pad)
	return l
}
