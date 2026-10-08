package screen

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestAroundAndScaled(t *testing.T) {
	b := image.Rect(-1920, 0, 2560, 1440) // два монитора, левый — отрицательный
	if r := Around(1000, 700, 600, 400, b); r != image.Rect(700, 500, 1300, 900) {
		t.Error(r)
	}
	if r := Around(2500, 20, 600, 400, b); r != image.Rect(2200, 0, 2560, 220) {
		t.Error("у края прижимается:", r)
	}
	if r := Around(-1900, 700, 600, 400, b); r.Min.X != -1920 {
		t.Error(r)
	}
	if r := Around(9000, 9000, 600, 400, b); !r.Empty() {
		t.Error(r)
	}
	if w, h := Scaled(144); w != 900 || h != 600 {
		t.Error(w, h)
	}
	if w, h := Scaled(0); w != 600 || h != 400 {
		t.Error(w, h)
	}
}

func TestUpscaleAndPNG(t *testing.T) {
	src := FromBGRA([]byte{255, 0, 0, 7, 0, 0, 255, 7}, 2, 1) // синий, красный
	if src.RGBAAt(0, 0) != (color.RGBA{0, 0, 255, 255}) || src.RGBAAt(1, 0) != (color.RGBA{255, 0, 0, 255}) {
		t.Fatal(src.RGBAAt(0, 0), src.RGBAAt(1, 0))
	}
	up := Upscale2(src)
	if up.Bounds().Dx() != 4 || up.Bounds().Dy() != 2 {
		t.Fatal(up.Bounds())
	}
	if up.RGBAAt(0, 0) != (color.RGBA{0, 0, 255, 255}) || up.RGBAAt(3, 1) != (color.RGBA{255, 0, 0, 255}) {
		t.Error("края сохраняют цвет", up.RGBAAt(0, 0), up.RGBAAt(3, 1))
	}
	if m := up.RGBAAt(1, 0); m.R == 0 || m.B == 0 {
		t.Error("середина смешана", m)
	}
	big := image.NewRGBA(image.Rect(0, 0, 1400, 10))
	if Upscale2(big) != big {
		t.Error("больше предела OCR не увеличиваем")
	}
	p := filepath.Join(t.TempDir(), "a", FileName)
	if err := SavePNG(p, up); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(p)
	defer f.Close()
	if img, err := png.Decode(f); err != nil || img.Bounds().Dx() != 4 {
		t.Fatal(err)
	}
}

func TestFrameByMonitorHeight(t *testing.T) {
	mon := image.Rect(0, 0, 1920, 1080)
	if r := Frame(1100, 650, 96, mon); r.Dx() != 600 || r.Dy() != 454 || r.Min != image.Pt(800, 423) {
		t.Error("1080p:", r)
	}
	// 4K при 150 %: по высоте больше, чем по масштабу.
	if r := Frame(1920, 1080, 144, image.Rect(0, 0, 3840, 2160)); r.Dx() != 1188 || r.Dy() != 907 {
		t.Error("4K:", r)
	}
	// Маленький монитор при большом масштабе — не меньше, чем по масштабу.
	if r := Frame(600, 400, 192, image.Rect(0, 0, 1280, 800)); r.Dx() != 1200 || r.Dy() != 800 {
		t.Error("масштаб:", r)
	}
	// Монитор не узнан — как раньше, 600×400 точек.
	if r := Frame(1000, 700, 96, image.Rectangle{}); !r.Empty() {
		t.Error("пустые границы — пустая рамка", r)
	}
	if r := Frame(10, 10, 96, mon); r.Min != image.Pt(0, 0) {
		t.Error("у края прижимается:", r)
	}
}

func TestFactorAndUpscale3(t *testing.T) {
	cases := [][3]int{{594, 454, 3}, {600, 400, 3}, {792, 605, 2}, {1188, 907, 2}, {1400, 900, 1}, {0, 0, 1}}
	for _, c := range cases {
		if k := Factor(c[0], c[1]); k != c[2] {
			t.Errorf("%dx%d: %d, ждали %d", c[0], c[1], k, c[2])
		}
	}
	src := FromBGRA([]byte{255, 0, 0, 7, 0, 0, 255, 7}, 2, 1)
	up := Upscale(src, 3)
	if up.Bounds().Dx() != 6 || up.Bounds().Dy() != 3 || up.RGBAAt(0, 0) != (color.RGBA{0, 0, 255, 255}) || up.RGBAAt(5, 2) != (color.RGBA{255, 0, 0, 255}) {
		t.Fatal(up.Bounds(), up.RGBAAt(0, 0), up.RGBAAt(5, 2))
	}
	if Upscale(src, 1) != src {
		t.Error("×1 — как есть")
	}
}

func TestBlackAndStale(t *testing.T) {
	black := make([]byte, 4*100)
	for i := 3; i < len(black); i += 4 {
		black[i] = 255
	}
	if !Black(black) {
		t.Error("чёрный")
	}
	img := append([]byte(nil), black...)
	for i := 0; i < 4*5; i++ {
		img[i] = 200 // 5 % светлых пикселей — уже не чёрный
	}
	if Black(img) || Black(nil) {
		t.Error("не чёрный")
	}
	var s Stale
	if s.Check(black, 10, 10) != EmptyBlack || s.Check(img, 10, 10) != "" || s.Check(img, 10, 10) != EmptySame {
		t.Error("застывший кадр")
	}
	img2 := append([]byte(nil), img...)
	img2[3] = 0 // альфа не в счёт
	img2[0] = 199
	if s.Check(img2, 10, 10) != "" {
		t.Error("другой кадр")
	}
	if Hash(img, 10, 10) == Hash(img, 20, 5) {
		t.Error("размер в отпечатке")
	}
}

func TestGrayVariants(t *testing.T) {
	// Тёмный фон, светлый «текст» в одном столбце из десяти.
	src := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			c := color.RGBA{40, 30, 20, 255}
			if x == 4 {
				c = color.RGBA{220, 200, 150, 255}
			}
			src.SetRGBA(x, y, c)
		}
	}
	g := Gray(src, false)
	if g.GrayAt(0, 0).Y != 0 || g.GrayAt(4, 0).Y != 255 {
		t.Error("контраст растянут:", g.GrayAt(0, 0), g.GrayAt(4, 0))
	}
	inv := Gray(src, true)
	if inv.GrayAt(0, 0).Y != 255 || inv.GrayAt(4, 0).Y != 0 {
		t.Error("инверсия:", inv.GrayAt(0, 0), inv.GrayAt(4, 0))
	}
	dir := t.TempDir()
	p := filepath.Join(dir, FileName)
	if err := SavePNG(p, src); err != nil {
		t.Fatal(err)
	}
	// Из файла (в памяти другого снимка нет) и из памяти.
	for _, remember := range []bool{false, true} {
		if remember {
			Remember(p, src)
		}
		dst := filepath.Join(dir, "v.png")
		if err := Variant(p, dst, VarInvert); err != nil {
			t.Fatal(err)
		}
		f, _ := os.Open(dst)
		m, err := png.Decode(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if r, _, _, _ := m.At(4, 0).RGBA(); r != 0 {
			t.Error("вариант в файле:", m.At(4, 0))
		}
	}
	if Variant(p, filepath.Join(dir, "x.png"), "bogus") == nil {
		t.Error("неизвестный вариант")
	}
}
