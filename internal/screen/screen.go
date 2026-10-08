// Пакет screen — снимок области экрана вокруг курсора для карточки зоны
// (как Экран.вокругКурсора у мака: 600×400 точек, а не весь экран — быстрее
// и меньше постороннего текста лезет в распознавание).
//
// Здесь — расчёт рамки, увеличение и PNG (проверяется на маке); сам снимок
// GDI — в screen_windows.go.
package screen

import (
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
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

// Upscale2 увеличивает картинку вдвое (билинейно). Мелкий текст тултипа
// (14–16 пикселей при 100 %) OCR Windows читает заметно хуже крупного.
// Если вдвое не влезает в MaxOCR — отдаёт как есть.
func Upscale2(src *image.RGBA) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 || 2*w > MaxOCR || 2*h > MaxOCR {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, 2*w, 2*h))
	at := func(x, y int) []uint8 {
		x, y = min(max(x, 0), w-1), min(max(y, 0), h-1)
		i := src.PixOffset(b.Min.X+x, b.Min.Y+y)
		return src.Pix[i : i+4]
	}
	for y := 0; y < 2*h; y++ {
		// центр пикселя назначения в координатах исходника
		fy := (float64(y)+0.5)/2 - 0.5
		y0 := int(fy)
		if fy < 0 {
			y0 = -1
		}
		wy := fy - float64(y0)
		for x := 0; x < 2*w; x++ {
			fx := (float64(x)+0.5)/2 - 0.5
			x0 := int(fx)
			if fx < 0 {
				x0 = -1
			}
			wx := fx - float64(x0)
			p00, p10, p01, p11 := at(x0, y0), at(x0+1, y0), at(x0, y0+1), at(x0+1, y0+1)
			o := dst.PixOffset(x, y)
			for c := 0; c < 4; c++ {
				top := float64(p00[c])*(1-wx) + float64(p10[c])*wx
				bot := float64(p01[c])*(1-wx) + float64(p11[c])*wx
				dst.Pix[o+c] = uint8(top*(1-wy) + bot*wy + 0.5)
			}
		}
	}
	return dst
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
