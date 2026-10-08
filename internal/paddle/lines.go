package paddle

import (
	"image"
	"sort"
)

// Деление картинки на строки текста — вместо детектора PaddleOCR: тултип
// игры уже вырезан (internal/screen), фон у него тёмный, текст светлый.
// Горизонтальная проекция «чернил» (пикселей заметно ярче фона) делит
// картинку на полосы; полоса, где строки слиплись через значок (вихрь
// портала слева тянется на две строки), делится по провалу проекции.
// В полосе — куски, разделённые широким пустым промежутком (значок,
// «7/7 … нет»): каждый распознаётся отдельно, а текст полосы собирается
// обратно в одну строку.

// Line — строка текста: рамка полосы и её куски слева направо.
type Line struct {
	Box  image.Rectangle
	Segs []image.Rectangle
}

const (
	inkOver  = 45 // «чернила» — ярче фона (медианы) хотя бы на столько
	inkFloor = 70 // и не темнее этого
	minLineH = 5  // ниже — не строка (крошки, рамка)
	maxLineH = 64 // выше — не строка текста тултипа (картинка, сцена)
	valley   = 0.2
	minSegW  = 3
	// maxChroma — у «чернил» разница каналов не больше этого (серое).
	maxChroma = 60
)

// lum — яркость пикселей картинки (0–255).
func lum(img *image.RGBA) ([]uint8, int, int) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	out := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		i := img.PixOffset(b.Min.X, b.Min.Y+y)
		for x := 0; x < w; x++ {
			p := img.Pix[i+4*x : i+4*x+3]
			out[y*w+x] = uint8((299*int(p[0]) + 587*int(p[1]) + 114*int(p[2]) + 500) / 1000)
		}
	}
	return out, w, h
}

func median(L []uint8) int {
	var hist [256]int
	for _, v := range L {
		hist[v]++
	}
	acc := 0
	for v, c := range hist {
		if acc += c; acc*2 >= len(L) {
			return v
		}
	}
	return 0
}

// Dark — фон картинки тёмный (медиана яркости не выше 70): светлый текст на
// тёмном, как у тултипа. Иначе делить на строки проекцией нельзя.
func Dark(img *image.RGBA) bool {
	L, _, _ := lum(img)
	return len(L) > 0 && median(L) <= 70
}

// Split делит картинку на строки текста (сверху вниз). Рамка тултипа
// (длинные светлые линии по краю) и одиночные крошки строками не считаются.
func Split(img *image.RGBA) []Line {
	L, w, h := lum(img)
	if w == 0 || h == 0 {
		return nil
	}
	thr := uint8(min(max(median(L)+inkOver, inkFloor), 250))
	// Текст тултипа белый и серый; дороги карты под полупрозрачным
	// тултипом, полоска размера и вихрь портала — цветные: их не берём.
	ink := make([]bool, w*h)
	b := img.Bounds()
	for y := 0; y < h; y++ {
		p := img.Pix[img.PixOffset(b.Min.X, b.Min.Y+y):]
		for x := 0; x < w; x++ {
			r, g, bl := int(p[4*x]), int(p[4*x+1]), int(p[4*x+2])
			chroma := max(r, g, bl) - min(r, g, bl)
			ink[y*w+x] = L[y*w+x] >= thr && chroma <= maxChroma
		}
	}
	// Полоски во всю ширину (край рамки тултипа, разделитель) — не текст:
	// убираем строки, где «чернила» идут сплошь больше чем на 90 % ширины.
	rows := make([]int, h)
	for y := 0; y < h; y++ {
		run, best, n := 0, 0, 0
		for x := 0; x < w; x++ {
			if ink[y*w+x] {
				n++
				run++
				best = max(best, run)
			} else {
				run = 0
			}
		}
		if best*10 > w*9 {
			for x := 0; x < w; x++ {
				ink[y*w+x] = false
			}
			n = 0
		}
		rows[y] = n
	}
	var bands [][2]int
	for _, b := range runsOf(rows, 1) {
		bands = append(bands, splitValleys(rows, b[0], b[1])...)
	}
	var out []Line
	for _, b := range bands {
		y0, y1 := b[0], b[1]+1
		if y1-y0 < minLineH || y1-y0 > maxLineH {
			continue
		}
		cols := make([]int, w)
		for y := y0; y < y1; y++ {
			for x := 0; x < w; x++ {
				if ink[y*w+x] {
					cols[x]++
				}
			}
		}
		// Куски полосы: промежуток шире высоты строки — другой кусок.
		gap := max(y1-y0, 6)
		var segs []image.Rectangle
		for _, s := range runsGap(cols, gap) {
			// Уже своей высоты — крошка или одиночный значок, не слово.
			if s[1]-s[0]+1 < max(minSegW, y1-y0) {
				continue
			}
			segs = append(segs, image.Rect(s[0], y0, s[1]+1, y1))
		}
		if len(segs) == 0 {
			continue
		}
		box := segs[0]
		for _, s := range segs[1:] {
			box = box.Union(s)
		}
		out = append(out, Line{Box: box, Segs: segs})
	}
	return out
}

// runsOf — отрезки подряд, где v[i] ≥ least.
func runsOf(v []int, least int) [][2]int {
	var out [][2]int
	start := -1
	for i, n := range v {
		if n >= least {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			out = append(out, [2]int{start, i - 1})
			start = -1
		}
	}
	if start >= 0 {
		out = append(out, [2]int{start, len(v) - 1})
	}
	return out
}

// runsGap — отрезки с «чернилами», разрывы короче gap прощаются.
func runsGap(v []int, gap int) [][2]int {
	var out [][2]int
	start, last := -1, -1
	for i, n := range v {
		if n == 0 {
			continue
		}
		if start >= 0 && i-last-1 >= gap {
			out = append(out, [2]int{start, last})
			start = -1
		}
		if start < 0 {
			start = i
		}
		last = i
	}
	if start >= 0 {
		out = append(out, [2]int{start, last})
	}
	return out
}

// splitValleys делит полосу [a, b] по провалам проекции: строки, где
// «чернил» не больше valley от самой полной строки полосы, и выше и ниже
// которых есть полные строки, — граница между строками текста (их
// склеил значок сбоку). Провалы у края полосы (хвосты букв у, р, д) не
// делят.
func splitValleys(rows []int, a, b int) [][2]int {
	peak := 0
	for y := a; y <= b; y++ {
		peak = max(peak, rows[y])
	}
	low := func(y int) bool { return float64(rows[y]) <= valley*float64(peak) }
	var cuts [][2]int // провалы внутри полосы
	for y := a; y <= b; {
		if !low(y) {
			y++
			continue
		}
		z := y
		for z+1 <= b && low(z+1) {
			z++
		}
		if y > a && z < b {
			cuts = append(cuts, [2]int{y, z})
		}
		y = z + 1
	}
	if len(cuts) == 0 {
		return [][2]int{{a, b}}
	}
	var out [][2]int
	start := a
	for _, c := range cuts {
		if c[0]-start >= minLineH && b-c[1] >= minLineH {
			out = append(out, [2]int{start, c[0] - 1})
			start = c[1] + 1
		}
	}
	out = append(out, [2]int{start, b})
	// Части из одних провалов по краям (если остались) — отрезаем.
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}
