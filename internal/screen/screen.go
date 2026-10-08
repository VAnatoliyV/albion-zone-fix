// Пакет screen — снимок области экрана вокруг курсора для карточки зоны
// (как Экран.вокругКурсора у мака: рамка с центром в курсоре, а не весь
// экран — быстрее и меньше постороннего текста лезет в распознавание).
//
// Здесь — расчёт рамки, увеличение, варианты картинки для OCR, проверка
// «пустого» снимка и PNG (проверяется на маке); сам снимок GDI — в
// screen_windows.go.
package screen

import (
	"errors"
	"hash/fnv"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sync"
)

// Размер области в логических точках (при 100 % масштаба — в пикселях).
const (
	Width  = 600
	Height = 400
)

// MaxOCR — больше этого по любой стороне OCR Windows картинку не берёт
// (OcrEngine.MaxImageDimension = 2600).
const MaxOCR = 2600

// ErrUnsupported — снимок экрана есть только на Windows.
var ErrUnsupported = errors.New("снимок экрана есть только в Windows")

// FileName — последний снимок в каталоге данных (перезаписывается; по нему
// видно, что именно попало в распознавание).
const FileName = "zone-capture.png"

// Around — рамка w×h с центром в (cx, cy), прижатая к границам bounds
// (весь виртуальный экран: несколько мониторов). Пустая — курсор вне экрана.
func Around(cx, cy, w, h int, bounds image.Rectangle) image.Rectangle {
	r := image.Rect(cx-w/2, cy-h/2, cx-w/2+w, cy-h/2+h)
	return r.Intersect(bounds)
}

// Scaled — размер области в пикселях при масштабе экрана dpi (96 = 100 %).
func Scaled(dpi int) (int, int) {
	if dpi <= 0 {
		dpi = 96
	}
	return Width * dpi / 96, Height * dpi / 96
}

// Доля высоты монитора под рамку: интерфейс игры масштабируется от высоты
// экрана (1080p — база), и тултип портала на 1440p/4K крупнее, чем 600×400
// точек при 100 % масштаба Windows. При 1080p и 100 % — 600×454.
const (
	FrameW = 0.55
	FrameH = 0.42
)

// Frame — рамка снимка: с центром в курсоре (как у мака — тултип портала
// Albion рисуется рядом с курсором то справа, то слева, то сверху, то снизу,
// смотря где край экрана), размер — больший из «по высоте монитора» и «по
// масштабу Windows», прижата к монитору mon. Пустой mon — по масштабу.
func Frame(cx, cy, dpi int, mon image.Rectangle) image.Rectangle {
	w, h := Scaled(dpi)
	if H := mon.Dy(); H > 0 {
		w = max(w, int(FrameW*float64(H)+0.5))
		h = max(h, int(FrameH*float64(H)+0.5))
	}
	return Around(cx, cy, w, h, mon)
}

// OCRSide — к этому размеру по длинной стороне увеличиваем варианты
// картинки (серый, инверсия) при повторах: мелкий текст тултипа (14–16
// пикселей при 1080p) OCR Windows читает хуже крупного, а слишком большая
// картинка распознаётся дольше. Первый, цветной проход — ×2, как раньше.
const OCRSide = 1800

// Factor — во сколько раз увеличить снимок w×h для первого (цветного)
// прохода OCR: ×2, пока влезает в MaxOCR, иначе 1.
func Factor(w, h int) int {
	side := max(w, h)
	if side > 0 && 2*side <= MaxOCR {
		return 2
	}
	return 1
}

// VariantFactor — увеличение вариантов: ×3, пока длинная сторона не больше
// OCRSide, иначе как Factor.
func VariantFactor(w, h int) int {
	if side := max(w, h); side > 0 && 3*side <= OCRSide {
		return 3
	}
	return Factor(w, h)
}

// Upscale2 увеличивает картинку вдвое (билинейно). Мелкий текст тултипа
// (14–16 пикселей при 100 %) OCR Windows читает заметно хуже крупного.
// Если вдвое не влезает в MaxOCR — отдаёт как есть.
func Upscale2(src *image.RGBA) *image.RGBA { return Upscale(src, 2) }

// Upscale увеличивает картинку в k раз (билинейно). k < 2 или не влезает в
// MaxOCR — отдаёт как есть.
func Upscale(src *image.RGBA, k int) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if k < 2 || w == 0 || h == 0 || k*w > MaxOCR || k*h > MaxOCR {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, k*w, k*h))
	upscalePix(src.Pix[src.PixOffset(b.Min.X, b.Min.Y):], src.Stride, w, h, 4, k, dst.Pix)
	return dst
}

// UpscaleGray — то же для серой картинки.
func UpscaleGray(src *image.Gray, k int) *image.Gray {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if k < 2 || w == 0 || h == 0 || k*w > MaxOCR || k*h > MaxOCR {
		return src
	}
	dst := image.NewGray(image.Rect(0, 0, k*w, k*h))
	upscalePix(src.Pix[src.PixOffset(b.Min.X, b.Min.Y):], src.Stride, w, h, 1, k, dst.Pix)
	return dst
}

// upscalePix — билинейное увеличение в k раз: ch каналов на пиксель,
// строки исходника через stride, назначение плотное (k*w*ch на строку).
func upscalePix(src []uint8, stride, w, h, ch, k int, dst []uint8) {
	kf := float64(k)
	at := func(x, y int) int {
		x, y = min(max(x, 0), w-1), min(max(y, 0), h-1)
		return y*stride + x*ch
	}
	for y := 0; y < k*h; y++ {
		// центр пикселя назначения в координатах исходника
		fy := (float64(y)+0.5)/kf - 0.5
		y0 := int(fy)
		if fy < 0 {
			y0 = -1
		}
		wy := fy - float64(y0)
		for x := 0; x < k*w; x++ {
			fx := (float64(x)+0.5)/kf - 0.5
			x0 := int(fx)
			if fx < 0 {
				x0 = -1
			}
			wx := fx - float64(x0)
			i00, i10, i01, i11 := at(x0, y0), at(x0+1, y0), at(x0, y0+1), at(x0+1, y0+1)
			o := (y*k*w + x) * ch
			for c := 0; c < ch; c++ {
				top := float64(src[i00+c])*(1-wx) + float64(src[i10+c])*wx
				bot := float64(src[i01+c])*(1-wx) + float64(src[i11+c])*wx
				dst[o+c] = uint8(top*(1-wy) + bot*wy + 0.5)
			}
		}
	}
}

// SavePNG пишет картинку через временный файл.
func SavePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(f, img); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// FromBGRA — картинка из снимка GDI (32 бита, сверху вниз, порядок B G R A;
// альфа у экрана мусорная — ставим 255).
func FromBGRA(pix []byte, w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i+3 < len(pix) && i+3 < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = pix[i+2], pix[i+1], pix[i], 255
	}
	return img
}

// Пустой снимок: GDI в «настоящем» полноэкранном режиме игры может отдать
// чёрный кадр или кадр, застывший с прошлого раза.
const (
	EmptyBlack = "black" // почти весь чёрный
	EmptySame  = "same"  // пиксель в пиксель как прошлый снимок
)

// Black — почти весь снимок чёрный: не меньше 98 % пикселей темнее 16 по
// всем каналам (BGRA или RGBA — всё равно).
func Black(pix []byte) bool {
	n, dark := 0, 0
	for i := 0; i+3 < len(pix); i += 4 {
		n++
		if pix[i] < 16 && pix[i+1] < 16 && pix[i+2] < 16 {
			dark++
		}
	}
	return n > 0 && dark*100 >= n*98
}

// Hash — отпечаток пикселей (без альфы — у экрана она мусорная) и размера.
func Hash(pix []byte, w, h int) uint64 {
	f := fnv.New64a()
	f.Write([]byte{byte(w), byte(w >> 8), byte(h), byte(h >> 8)})
	buf := make([]byte, 0, 3*4096)
	for i := 0; i+3 < len(pix); i += 4 {
		buf = append(buf, pix[i], pix[i+1], pix[i+2])
		if len(buf) >= cap(buf)-3 {
			f.Write(buf)
			buf = buf[:0]
		}
	}
	f.Write(buf)
	return f.Sum64()
}

// Stale помнит отпечаток прошлого снимка: Check — пустой ли новый
// (EmptyBlack, EmptySame или "").
type Stale struct {
	mu   sync.Mutex
	last uint64
}

func (s *Stale) Check(pix []byte, w, h int) string {
	if Black(pix) {
		return EmptyBlack
	}
	sum := Hash(pix, w, h)
	s.mu.Lock()
	defer s.mu.Unlock()
	same := s.last == sum
	s.last = sum
	if same {
		return EmptySame
	}
	return ""
}

// Варианты картинки для OCR, кроме цветной «как снято».
const (
	VarGray   = "gray" // оттенки серого с растянутым контрастом
	VarInvert = "inv"  // то же, наоборот: тёмный текст на светлом
	VarBin    = "bin"  // порог: светлый текст — чёрным на белом
	VarFull   = "full" // вся рамка, когда снимок обрезан по тултипу
)

// Gray — оттенки серого, контраст растянут так, что 2 % самых тёмных
// пикселей становятся чёрными, 2 % самых светлых — белыми. invert —
// наоборот (светлый текст тултипа на тёмном фоне становится тёмным на
// светлом — так OCR обычно читает увереннее).
func Gray(src *image.RGBA, invert bool) *image.Gray {
	b := src.Bounds()
	dst := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
	var hist [256]int
	for y := 0; y < b.Dy(); y++ {
		i := src.PixOffset(b.Min.X, b.Min.Y+y)
		o := dst.PixOffset(0, y)
		for x := 0; x < b.Dx(); x++ {
			p := src.Pix[i+4*x : i+4*x+3]
			l := uint8((299*int(p[0]) + 587*int(p[1]) + 114*int(p[2]) + 500) / 1000)
			dst.Pix[o+x] = l
			hist[l]++
		}
	}
	n := b.Dx() * b.Dy()
	lo, hi := 0, 255
	for acc := 0; lo < 255; lo++ {
		if acc += hist[lo]; acc*100 > n*2 {
			break
		}
	}
	for acc := 0; hi > 0; hi-- {
		if acc += hist[hi]; acc*100 > n*2 {
			break
		}
	}
	var lut [256]uint8
	for v := 0; v < 256; v++ {
		x := v
		if hi > lo {
			x = (v - lo) * 255 / (hi - lo)
		}
		x = min(max(x, 0), 255)
		if invert {
			x = 255 - x
		}
		lut[v] = uint8(x)
	}
	for i, v := range dst.Pix {
		dst.Pix[i] = lut[v]
	}
	return dst
}

// Последний снимок в памяти — исходная рамка без увеличения и где в ней
// тултип: варианты делаются из неё без чтения PNG (серый считается на малой
// картинке и потом увеличивается).
var (
	lastMu   sync.Mutex
	lastPath string
	lastImg  *image.RGBA
	lastCrop image.Rectangle // пусто — снимок не обрезан
	lastK    int             // увеличение обрезки
)

// Remember — снимок рамки raw (без увеличения) записан в path (для Variant).
func Remember(path string, img *image.RGBA) { RememberCrop(path, img, image.Rectangle{}, 0) }

// RememberCrop — то же, но в path записана обрезка crop рамки raw,
// увеличенная в k раз: варианты делаются из той же обрезки, VarFull — из
// всей рамки.
func RememberCrop(path string, img *image.RGBA, crop image.Rectangle, k int) {
	lastMu.Lock()
	lastPath, lastImg, lastCrop, lastK = path, img, crop, k
	lastMu.Unlock()
}

// Variant пишет в dst вариант kind (VarGray, VarBin, VarInvert, VarFull)
// снимка src.
func Variant(src, dst, kind string) error {
	switch kind {
	case VarGray, VarInvert, VarBin, VarFull:
	default:
		return errors.New("неизвестный вариант снимка: " + kind)
	}
	lastMu.Lock()
	img, crop, k := lastImg, lastCrop, lastK
	if lastPath != src {
		img = nil
	}
	lastMu.Unlock()
	if kind == VarFull {
		if img == nil || crop.Empty() {
			return errors.New("нет всей рамки: снимок не обрезан")
		}
		b := img.Bounds()
		return SavePNG(dst, Upscale(img, Factor(b.Dx(), b.Dy())))
	}
	if img != nil {
		if !crop.Empty() {
			img = Crop(img, crop)
		} else {
			b := img.Bounds()
			k = VariantFactor(b.Dx(), b.Dy())
		}
		return SavePNG(dst, UpscaleGray(grayKind(img, kind), k))
	}
	// В памяти нет — из файла (он уже увеличен).
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	m, err := png.Decode(f)
	f.Close()
	if err != nil {
		return err
	}
	return SavePNG(dst, grayKind(toRGBA(m), kind))
}

func grayKind(img *image.RGBA, kind string) *image.Gray {
	if kind == VarBin {
		return Bin(img)
	}
	return Gray(img, kind == VarInvert)
}

// Bin — порог по Оцу: светлый текст тултипа — чёрным, остальное — белым
// (OCR увереннее всего читает чёрное на белом без полутонов).
func Bin(src *image.RGBA) *image.Gray {
	g := Gray(src, false)
	var hist [256]int
	for _, v := range g.Pix {
		hist[v]++
	}
	n := len(g.Pix)
	sum := 0
	for i, c := range hist {
		sum += i * c
	}
	var sumB, wB int
	best, thr := -1.0, 128
	for t := 0; t < 256; t++ {
		wB += hist[t]
		if wB == 0 {
			continue
		}
		wF := n - wB
		if wF == 0 {
			break
		}
		sumB += t * hist[t]
		mB := float64(sumB) / float64(wB)
		mF := float64(sum-sumB) / float64(wF)
		if v := float64(wB) * float64(wF) * (mB - mF) * (mB - mF); v > best {
			best, thr = v, t
		}
	}
	for i, v := range g.Pix {
		if int(v) > thr {
			g.Pix[i] = 0
		} else {
			g.Pix[i] = 255
		}
	}
	return g
}

func toRGBA(m image.Image) *image.RGBA {
	if r, ok := m.(*image.RGBA); ok {
		return r
	}
	b := m.Bounds()
	r := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			r.Set(x, y, m.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return r
}
