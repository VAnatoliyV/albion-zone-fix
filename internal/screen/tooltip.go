package screen

import (
	"image"
	"sort"
)

// Тултип Albion — тёмный почти непрозрачный прямоугольник с прямыми краями
// рядом с курсором. Вся рамка снимка — это ещё и сцена игры с текстурами,
// которые Windows OCR читает мусором; мелкий текст тултипа на ней читается
// плохо. Находим прямоугольник тултипа и распознаём только его, крупнее.
//
// Как: прямые края — длинные ровные перепады яркости. Горизонтальные края —
// строки, где на длинном отрезке яркость сверху и снизу заметно разная;
// вертикальные — то же по столбцам. Сцена Albion изометрическая: длинных
// ровных горизонталей и вертикалей в ней почти нет, а у тултипа они есть.
// Из пар «верх — низ» берём самую большую рамку, у которой есть и боковые
// края, боковые края не продолжаются за верх и низ (иначе это полоска
// внутри тултипа), и курсор рядом.

const (
	edgeStep = 10 // перепад яркости через пиксель — край
	edgeGap  = 4  // разрыв края (буква, значок), который ещё прощаем
	sideHit  = 0.6
	// CropMargin — запас вокруг найденной рамки (пиксели снимка).
	CropMargin = 4
)

type hseg struct{ y, x0, x1 int }
type vseg struct{ x, y0, y1 int }

// lum — яркость пикселей (0–255), w×h.
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

func absDiff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

// runs — отрезки, где hit, с разрывами не длиннее edgeGap, не короче minLen.
func runs(n, minLen int, hit func(i int) bool, emit func(a, b int)) {
	start, last := -1, -1
	for i := 0; i < n; i++ {
		if !hit(i) {
			continue
		}
		if start >= 0 && i-last-1 > edgeGap {
			if last-start+1 >= minLen {
				emit(start, last)
			}
			start = -1
		}
		if start < 0 {
			start = i
		}
		last = i
	}
	if start >= 0 && last-start+1 >= minLen {
		emit(start, last)
	}
}

// FindTooltip — рамка тултипа в снимке img (в координатах img); false — не
// нашлась уверенно (тогда распознаём всю рамку).
func FindTooltip(img *image.RGBA) (image.Rectangle, bool) {
	L, w, h := lum(img)
	// Большая рамка (1440p, 4K) — ищем на уменьшенной вдвое: края те же,
	// а работы вчетверо меньше.
	k := 1
	for w > DetectSide {
		L, w, h = half(L, w, h)
		k *= 2
	}
	r, ok := findTooltip(L, w, h)
	if !ok {
		return image.Rectangle{}, false
	}
	r = image.Rect(r.Min.X*k, r.Min.Y*k, r.Max.X*k, r.Max.Y*k)
	b := img.Bounds()
	return r.Inset(-CropMargin).Intersect(image.Rect(0, 0, b.Dx(), b.Dy())).Add(b.Min), true
}

// DetectSide — шире этого рамка для поиска тултипа уменьшается вдвое.
const DetectSide = 700

// maxEdges — сколько самых длинных горизонтальных краёв перебирать парами
// (полосатая сцена — чат, текст — иначе даёт сотни краёв).
const maxEdges = 48

// half — яркость, уменьшенная вдвое (среднее 2×2).
func half(L []uint8, w, h int) ([]uint8, int, int) {
	w2, h2 := w/2, h/2
	out := make([]uint8, w2*h2)
	for y := 0; y < h2; y++ {
		for x := 0; x < w2; x++ {
			i := 2*y*w + 2*x
			out[y*w2+x] = uint8((int(L[i]) + int(L[i+1]) + int(L[i+w]) + int(L[i+w+1]) + 2) / 4)
		}
	}
	return out, w2, h2
}

// meanIn — средняя яркость прямоугольника r (по сетке через 2 пикселя).
func meanIn(L []uint8, w int, r image.Rectangle) int {
	sum, n := 0, 0
	for y := r.Min.Y; y < r.Max.Y; y += 2 {
		for x := r.Min.X; x < r.Max.X; x += 2 {
			sum += int(L[y*w+x])
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / n
}

// findTooltip — рамка тултипа без запаса, в координатах L.
func findTooltip(L []uint8, w, h int) (image.Rectangle, bool) {
	if w < 120 || h < 80 {
		return image.Rectangle{}, false
	}
	minW, minH := max(w/6, 60), max(h/12, 30)
	hEdge := func(x, y int) bool {
		return y >= 1 && y+1 < h && absDiff(L[(y-1)*w+x], L[(y+1)*w+x]) >= edgeStep
	}
	vEdge := func(x, y int) bool {
		return x >= 1 && x+1 < w && absDiff(L[y*w+x-1], L[y*w+x+1]) >= edgeStep
	}
	var hs []hseg
	for y := 1; y+1 < h; y++ {
		runs(w, minW, func(x int) bool { return hEdge(x, y) }, func(a, b int) {
			// Край толщиной в 2–3 строки — один: оставляем длиннейший.
			if n := len(hs); n > 0 && hs[n-1].y >= y-2 && hs[n-1].x0 < b && a < hs[n-1].x1 {
				if b-a > hs[n-1].x1-hs[n-1].x0 {
					hs[n-1] = hseg{y, a, b}
				}
				return
			}
			hs = append(hs, hseg{y, a, b})
		})
	}
	if len(hs) > maxEdges {
		sort.SliceStable(hs, func(i, j int) bool { return hs[i].x1-hs[i].x0 > hs[j].x1-hs[j].x0 })
		hs = hs[:maxEdges]
		sort.SliceStable(hs, func(i, j int) bool { return hs[i].y < hs[j].y })
	}
	// Доля строк [y0, y1], где у столбца x (±rad) есть вертикальный край,
	// и лучший столбец.
	side := func(x, rad, y0, y1 int) (float64, int) {
		bestX, best := x, -1
		for dx := -rad; dx <= rad; dx++ {
			xx := x + dx
			if xx < 1 || xx+1 >= w {
				continue
			}
			n := 0
			for y := max(y0, 0); y <= min(y1, h-1); y++ {
				if vEdge(xx, y) {
					n++
				}
			}
			if n > best {
				best, bestX = n, xx
			}
		}
		if y1 < y0 || best < 0 {
			return 0, x
		}
		return float64(best) / float64(y1-y0+1), bestX
	}
	cx, cy := w/2, h/2
	reach := max(w, h) / 10
	// Из подходящих рамок: тёмная (тултип — тёмная плашка: внутри темнее,
	// чем вокруг) важнее светлой; среди равных — ближе к курсору (центр
	// снимка), и только потом крупнее. Иначе большая панель рядом (чат,
	// окно) перебивала тултип.
	type cand struct {
		r    image.Rectangle
		dark bool
		dist int
		area int
	}
	var found *cand
	better := func(a, b cand) bool {
		if a.dark != b.dark {
			return a.dark
		}
		if a.dist != b.dist {
			return a.dist < b.dist
		}
		return a.area > b.area
	}
	frame := image.Rect(0, 0, w, h)
	for i, top := range hs {
		for _, bot := range hs[i+1:] {
			if bot.y-top.y < minH {
				continue
			}
			lo, hi := max(top.x0, bot.x0), min(top.x1, bot.x1)
			wide := max(top.x1-top.x0, bot.x1-bot.x0)
			if hi-lo < wide*8/10 {
				continue // верх и низ разной ширины — не одна рамка
			}
			y0, y1 := top.y+2, bot.y-2
			fl, xl := side(lo, 8, y0, y1)
			fr, xr := side(hi, 8, y0, y1)
			if fl < sideHit || fr < sideHit || xr-xl < minW {
				continue
			}
			// Боковые края продолжаются за верх или низ — это полоска
			// внутри тултипа или кусок чего-то большего.
			if a, _ := side(xl, 1, bot.y+4, bot.y+16); a >= sideHit {
				if b, _ := side(xr, 1, bot.y+4, bot.y+16); b >= sideHit {
					continue
				}
			}
			if a, _ := side(xl, 1, top.y-16, top.y-4); a >= sideHit {
				if b, _ := side(xr, 1, top.y-16, top.y-4); b >= sideHit {
					continue
				}
			}
			r := image.Rect(xl, top.y, xr+1, bot.y+1)
			if r.Dx()*r.Dy()*10 > w*h*7 {
				continue // почти вся рамка — не тултип
			}
			if !image.Pt(cx, cy).In(r.Inset(-reach)) {
				continue // тултип рисуется у курсора, а курсор — в центре снимка
			}
			ring := r.Inset(-8).Intersect(frame)
			in := meanIn(L, w, r.Inset(3))
			out := (meanIn(L, w, ring)*ring.Dx()*ring.Dy() - in*r.Dx()*r.Dy()) / max(ring.Dx()*ring.Dy()-r.Dx()*r.Dy(), 1)
			c := cand{r: r, dark: in+10 < out, dist: distTo(r, cx, cy) / 8, area: r.Dx() * r.Dy()}
			if found == nil || better(c, *found) {
				found = &c
			}
		}
	}
	if found == nil {
		return image.Rectangle{}, false
	}
	return found.r, true
}

// distTo — расстояние от точки (x, y) до прямоугольника r (0 — внутри).
func distTo(r image.Rectangle, x, y int) int {
	dx := max(r.Min.X-x, 0, x-(r.Max.X-1))
	dy := max(r.Min.Y-y, 0, y-(r.Max.Y-1))
	return max(dx, dy)
}

// CropFactor — увеличение обрезки по тултипу: ×4, пока длинная сторона не
// больше CropSide, иначе ×3, ×2.
const CropSide = 2000

// CropFactorFor — во сколько раз увеличить обрезку w×h.
func CropFactorFor(w, h int) int {
	side := max(w, h)
	for k := 4; k >= 2; k-- {
		if side > 0 && k*side <= CropSide {
			return k
		}
	}
	return 1
}

// Crop — копия части r картинки img (начало координат — 0, 0).
func Crop(img *image.RGBA, r image.Rectangle) *image.RGBA {
	r = r.Intersect(img.Bounds())
	out := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	for y := 0; y < r.Dy(); y++ {
		copy(out.Pix[y*out.Stride:y*out.Stride+4*r.Dx()], img.Pix[img.PixOffset(r.Min.X, r.Min.Y+y):])
	}
	return out
}
