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
