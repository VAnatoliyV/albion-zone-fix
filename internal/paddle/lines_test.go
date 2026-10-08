package paddle

import (
	"image"
	"math"
	"testing"

	"albionzonefix/internal/screen"
)

// tooltipOf — тултип снимка, как его отдаёт screen.Native: обрезка без
// запаса и рамки.
func tooltipOf(t testing.TB, name string, k int) *image.RGBA {
	t.Helper()
	img := Native(t, name, k)
	r, ok := screen.FindTooltip(img)
	if !ok {
		t.Fatalf("%s: тултип не найден", name)
	}
	return screen.Crop(img, r.Inset(screen.CropMargin+1))
}

// Настоящие снимки тестера: четыре строки тултипа — заголовок, название,
// полоска размера, время; вихрь портала слева не склеивает заголовок с
// названием, дороги карты под полупрозрачным тултипом — не текст.
func TestSplitRealTooltips(t *testing.T) {
	for _, name := range []string{"zone-capture.png", "zone-capture-doubt.png", "zone-capture-3.png"} {
		img := tooltipOf(t, name, 2)
		if !Dark(img) {
			t.Errorf("%s: фон тултипа не тёмный", name)
		}
		lines := Split(img)
		if len(lines) != 4 {
			t.Fatalf("%s: %d строк: %v", name, len(lines), lines)
		}
		for i, ln := range lines {
			if h := ln.Box.Dy(); h < 7 || h > 14 {
				t.Errorf("%s: строка %d высотой %d: %v", name, i, h, ln.Box)
			}
			if i > 0 && ln.Box.Min.Y < lines[i-1].Box.Max.Y {
				t.Errorf("%s: строки %d и %d налезают", name, i-1, i)
			}
		}
		// Заголовок: значок отдельным куском, текст — своим.
		if n := len(lines[0].Segs); n != 2 {
			t.Errorf("%s: заголовок из %d кусков: %v", name, n, lines[0].Segs)
		}
		// Время — справа, одним куском («Закроется через 7 ч 05 м»).
		if n := len(lines[3].Segs); n != 1 || lines[3].Box.Min.X < img.Bounds().Dx()/3 {
			t.Errorf("%s: строка времени %v", name, lines[3].Segs)
		}
	}
}

func TestSplitSynthetic(t *testing.T) {
	img := loadPNG(t, "testdata/syn-en-2.png")
	if got := len(Split(img)); got != 3 {
		t.Errorf("строк %d, ждали 3 (полоска размера оранжевая — не текст)", got)
	}
	// Пусто и ровный фон — ничего.
	if got := Split(image.NewRGBA(image.Rect(0, 0, 100, 40))); len(got) != 0 {
		t.Errorf("чёрная картинка: %v", got)
	}
	// Светлый фон — не наш случай.
	light := image.NewRGBA(image.Rect(0, 0, 50, 20))
	for i := range light.Pix {
		light.Pix[i] = 200
	}
	if Dark(light) {
		t.Error("светлая картинка названа тёмной")
	}
}

func TestSplitValleys(t *testing.T) {
	// Две строки, склеенные значком: провал посередине делит.
	rows := []int{50, 60, 55, 58, 52, 5, 4, 70, 80, 75, 72, 66}
	got := splitValleys(rows, 0, len(rows)-1)
	if len(got) != 2 || got[0] != [2]int{0, 4} || got[1] != [2]int{7, 11} {
		t.Errorf("деление %v", got)
	}
	// Хвосты букв внизу полосы не делят.
	rows = []int{50, 60, 55, 58, 52, 60, 4, 3}
	if got := splitValleys(rows, 0, len(rows)-1); len(got) != 1 {
		t.Errorf("хвосты поделили: %v", got)
	}
}

func TestCharsetAndDecode(t *testing.T) {
	chars := Charset("a\nb\r\nв\n")
	if len(chars) != 5 || chars[0] != "" || chars[3] != "в" || chars[4] != " " {
		t.Fatalf("словарь %q", chars)
	}
	// Шаги: a a blank a b (пробел) в в → «aa b в»… с повторами: a, a — один.
	steps := []int{1, 1, 0, 1, 2, 4, 3, 3}
	C := len(chars)
	probs := make([]float32, len(steps)*C)
	for i, s := range steps {
		probs[i*C+s] = 0.9
	}
	text, score := Decode(probs, len(steps), C, chars)
	if text != "aab в" || math.Abs(score-0.9) > 1e-6 {
		t.Errorf("декод %q %.3f", text, score)
	}
	if text, _ := Decode(make([]float32, 3*C), 3, C, chars); text != "" {
		t.Errorf("пустой декод %q", text)
	}
}

// Prepare: высота 48, ширина по пропорции, каналы BGR, (x/255−0.5)/0.5,
// справа нули.
func TestPrepare(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 20, 10))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 255, 0, 51, 255
	}
	W := 320
	dst := make([]float32, 3*Height*W)
	for i := range dst {
		dst[i] = 7
	}
	rw := Prepare(img, img.Bounds(), W, dst)
	if rw != 96 {
		t.Fatalf("ширина %d, ждали 96", rw)
	}
	plane := Height * W
	at := func(c, y, x int) float32 { return dst[c*plane+y*W+x] }
	near := func(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-4 }
	if !near(at(0, 10, 10), 51.0/255*2-1) || !near(at(1, 10, 10), -1) || !near(at(2, 10, 10), 1) {
		t.Errorf("BGR: %v %v %v", at(0, 10, 10), at(1, 10, 10), at(2, 10, 10))
	}
	if at(0, 0, 96) != 0 || at(2, 47, 319) != 0 {
		t.Error("справа не нули")
	}
}
