package overlay

import (
	"testing"

	"albionzonefix/internal/settings"
	"albionzonefix/internal/zonecard"
)

func TestPlaceCorners(t *testing.T) {
	// Монитор 1920×1080, панель задач снизу 40 точек; 100 %.
	work := Rect{0, 0, 1920, 1040}
	for corner, want := range map[string][2]int{
		settings.CornerTopRight:    {1920 - 24 - 300, 24},
		settings.CornerBottomRight: {1920 - 24 - 300, 1040 - 24 - 200},
		settings.CornerTopLeft:     {24, 24},
		settings.CornerBottomLeft:  {24, 1040 - 24 - 200},
		"":                         {1920 - 24 - 300, 24}, // по умолчанию — правый верхний
	} {
		if x, y := Place(work, 300, 200, corner, 96); x != want[0] || y != want[1] {
			t.Errorf("%q: %d,%d, а надо %v", corner, x, y, want)
		}
	}
}

func TestPlaceDPIAndSecondMonitor(t *testing.T) {
	// Второй монитор слева от основного, 150 %: отступ 36 пикселей.
	work := Rect{-2560, 0, 0, 1400}
	if x, y := Place(work, 450, 300, settings.CornerTopRight, 144); x != -36-450 || y != 36 {
		t.Fatalf("справа сверху: %d,%d", x, y)
	}
	if x, y := Place(work, 450, 300, settings.CornerBottomLeft, 144); x != -2560+36 || y != 1400-36-300 {
		t.Fatalf("слева снизу: %d,%d", x, y)
	}
	// Монитор выше основного (отрицательный верх), 125 %.
	work = Rect{0, -1080, 1920, 0}
	if x, y := Place(work, 375, 250, settings.CornerBottomRight, 120); x != 1920-30-375 || y != -30-250 {
		t.Fatalf("справа снизу: %d,%d", x, y)
	}
	// Панель больше экрана — не уезжает за левый и верхний край.
	if x, y := Place(Rect{0, 0, 200, 100}, 300, 200, settings.CornerBottomRight, 96); x != 0 || y != 0 {
		t.Fatalf("крошечный экран: %d,%d", x, y)
	}
}

func TestScale(t *testing.T) {
	for _, c := range [][3]int{{24, 96, 24}, {24, 120, 30}, {24, 144, 36}, {24, 192, 48}, {300, 0, 300}} {
		if got := Scale(c[0], c[1]); got != c[2] {
			t.Errorf("Scale(%d, %d)=%d", c[0], c[1], got)
		}
	}
}

func fakeMeasure(f Font, s string) int { return len([]rune(s)) * 7 }

func TestArrangeGrowsWithRowsAndScales(t *testing.T) {
	bare := zonecard.Panel{Title: "Qiient-Al-Vynsis", Color: 0x8A5ABF, Subtitle: "дорога убежищ · T6"}
	full := bare
	full.Rows = []zonecard.PanelRow{
		{Label: "лагеря", Items: []zonecard.PanelItem{{Color: 0x4E77C4, Text: "2 × средний ветеранский"}, {Color: 0x4FAF5A, Text: "1 × малый"}}},
		{Label: "на них", Items: []zonecard.PanelItem{{Color: 0x9DBF4A, Text: "волокно T4–T6 ×12"}}},
	}
	full.Footer, full.Warn = "портал на 7 · закроется через 4:00", true

	a, b := Arrange(bare, 96, fakeMeasure), Arrange(full, 96, fakeMeasure)
	if a.W != 300 || b.W != 300 || b.H <= a.H {
		t.Fatalf("размеры: %dx%d и %dx%d", a.W, a.H, b.W, b.H)
	}
	big := Arrange(full, 192, func(f Font, s string) int { return fakeMeasure(f, s) * 2 })
	if big.W != 600 || big.H < 2*b.H-2 || big.H > 2*b.H+2 {
		t.Fatalf("200 %%: %dx%d при %dx%d", big.W, big.H, b.W, b.H)
	}
	// Всё внутри панели, низ подсвечен золотом.
	for _, op := range b.Ops {
		if op.X < 0 || op.Y < 0 || op.X+op.W > b.W || op.Y+op.H > b.H {
			t.Errorf("за краем: %+v (панель %dx%d)", op, b.W, b.H)
		}
	}
	last := b.Ops[len(b.Ops)-1]
	if !last.IsText || last.Text != full.Footer || last.Color != ColorGold {
		t.Fatalf("низ: %+v", last)
	}
}

func TestArrangeWrapsItems(t *testing.T) {
	p := zonecard.Panel{Title: "X", Rows: []zonecard.PanelRow{{Label: "данжи", Items: []zonecard.PanelItem{
		{Text: "2 × одиночное подземелье"}, {Text: "1 × групповое подземелье"}, {Text: "есть выход в город туманов"}}}}}
	l := Arrange(p, 96, fakeMeasure)
	var ys []int
	for _, op := range l.Ops {
		if op.IsText && op.Font == FontText {
			ys = append(ys, op.Y)
		}
	}
	if len(ys) != 3 || ys[0] == ys[1] || ys[1] == ys[2] {
		t.Fatalf("каждый длинный значок — на своей строке: %v", ys)
	}
	for _, op := range l.Ops {
		if op.X+op.W > l.W-Scale(pad, 96) {
			t.Errorf("значок вылез за поле: %+v", op)
		}
	}
}
