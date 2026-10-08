package screen

import (
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// loadRaw — снимок из testdata, уменьшенный обратно до размера рамки на
// экране (в файле он увеличен ×k для OCR).
func loadRaw(t *testing.T, name string, k int) *image.RGBA {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	b := m.Bounds()
	src := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(src, src.Bounds(), m, b.Min, draw.Src)
	w, h := b.Dx()/k, b.Dy()/k
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			for c := 0; c < 4; c++ {
				s := 0
				for dy := 0; dy < k; dy++ {
					for dx := 0; dx < k; dx++ {
						s += int(src.Pix[src.PixOffset(x*k+dx, y*k+dy)+c])
					}
				}
				out.Pix[out.PixOffset(x, y)+c] = uint8(s / (k * k))
			}
		}
	}
	return out
}

// Настоящие снимки тестера (1080p, 600×454 и 600×400 точек; в файлах —
// увеличены ×2 и ×3). Рамка тултипа измерена на глаз по картинкам.
func TestFindTooltipRealCaptures(t *testing.T) {
	for _, c := range []struct {
		file string
		k    int
		want image.Rectangle
	}{
		{"zone-capture.png", 2, image.Rect(81, 157, 297, 225)},
		{"zone-capture-3.png", 2, image.Rect(81, 157, 297, 225)},
		{"zone-capture-doubt.png", 2, image.Rect(81, 131, 297, 198)},
		{"zone-capture-gray.png", 3, image.Rect(317, 86, 553, 222)},
		{"zone-capture-inv.png", 3, image.Rect(317, 86, 553, 222)},
	} {
		img := loadRaw(t, c.file, c.k)
		t0 := time.Now()
		got, ok := FindTooltip(img)
		took := time.Since(t0)
		t.Logf("%s: %v за %v", c.file, got, took)
		if !ok {
			t.Errorf("%s: тултип не найден", c.file)
			continue
		}
		// Найденная рамка (с запасом) содержит тултип и не намного больше.
		if !c.want.Inset(3).In(got) || !got.In(c.want.Inset(-(CropMargin + 6))) {
			t.Errorf("%s: рамка %v, тултип %v", c.file, got, c.want)
		}
	}
}

// Ложных рамок нет: ровный фон, шум, тёмная сцена без краёв, тултип вдали
// от курсора.
func TestFindTooltipRejects(t *testing.T) {
	fill := func(w, h int, f func(x, y int) uint8) *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				v := f(x, y)
				i := img.PixOffset(x, y)
				img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = v, v, v, 255
			}
		}
		return img
	}
	seed := uint32(1)
	noise := func(int, int) uint8 { seed = seed*1664525 + 1013904223; return uint8(seed >> 24) }
	box := func(r image.Rectangle) func(x, y int) uint8 {
		return func(x, y int) uint8 {
			if image.Pt(x, y).In(r) {
				return 20
			}
			return 60
		}
	}
	for name, img := range map[string]*image.RGBA{
		"ровный":    fill(600, 454, func(int, int) uint8 { return 30 }),
		"шум":       fill(600, 454, noise),
		"диагонали": fill(600, 454, func(x, y int) uint8 { return uint8((x + y) % 64 * 4) }),
		"вдали":     fill(600, 454, box(image.Rect(10, 10, 200, 70))),
		"почти всё": fill(600, 454, box(image.Rect(5, 5, 595, 449))),
		"мелкая":    fill(100, 60, box(image.Rect(10, 10, 90, 50))),
	} {
		if r, ok := FindTooltip(img); ok {
			t.Errorf("%s: лишняя рамка %v", name, r)
		}
	}
	// Рамка у курсора находится, с запасом CropMargin.
	want := image.Rect(100, 150, 298, 226)
	r, ok := FindTooltip(fill(600, 454, box(want)))
	if !ok || !want.In(r) || !r.In(want.Inset(-CropMargin-2)) {
		t.Errorf("рамка у курсора: %v %v", r, ok)
	}
}

func TestCropAndFactor(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	img.Pix[img.PixOffset(3, 4)] = 200
	c := Crop(img, image.Rect(3, 4, 6, 8))
	if c.Bounds() != image.Rect(0, 0, 3, 4) || c.Pix[0] != 200 {
		t.Fatal(c.Bounds(), c.Pix[0])
	}
	if CropFactorFor(224, 76) != 4 || CropFactorFor(600, 200) != 3 || CropFactorFor(900, 300) != 2 {
		t.Error(CropFactorFor(224, 76), CropFactorFor(600, 200), CropFactorFor(900, 300))
	}
}

// Варианты обрезанного снимка — из той же обрезки и с тем же увеличением;
// VarFull — вся рамка; порог — светлый текст чёрным на белом.
func TestCroppedVariants(t *testing.T) {
	raw := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for i := range raw.Pix {
		raw.Pix[i] = 30
	}
	for y := 40; y < 44; y++ { // «текст»
		for x := 60; x < 90; x++ {
			i := raw.PixOffset(x, y)
			raw.Pix[i], raw.Pix[i+1], raw.Pix[i+2] = 230, 230, 230
		}
	}
	dir := t.TempDir()
	p := filepath.Join(dir, FileName)
	if err := SavePNG(p, raw); err != nil {
		t.Fatal(err)
	}
	crop := image.Rect(50, 30, 110, 60)
	RememberCrop(p, raw, crop, 4)
	size := func(kind string) image.Rectangle {
		dst := filepath.Join(dir, kind+".png")
		if err := Variant(p, dst, kind); err != nil {
			t.Fatal(kind, err)
		}
		f, _ := os.Open(dst)
		defer f.Close()
		m, err := png.Decode(f)
		if err != nil {
			t.Fatal(err)
		}
		if kind == VarBin {
			if g, _, _, _ := m.At(4*15, 4*11).RGBA(); g != 0 { // текст — чёрный
				t.Error("порог: текст", m.At(4*15, 4*11))
			}
			if g, _, _, _ := m.At(2, 2).RGBA(); g != 0xffff { // фон — белый
				t.Error("порог: фон", m.At(2, 2))
			}
		}
		return m.Bounds()
	}
	for _, k := range []string{VarGray, VarInvert, VarBin} {
		if b := size(k); b.Dx() != 60*4 || b.Dy() != 30*4 {
			t.Error(k, b)
		}
	}
	if b := size(VarFull); b.Dx() != 400 {
		t.Error("вся рамка ×2:", b)
	}
	// Не обрезан — всей рамки отдельно нет.
	Remember(p, raw)
	if Variant(p, filepath.Join(dir, "f.png"), VarFull) == nil {
		t.Error("VarFull без обрезки")
	}
}
