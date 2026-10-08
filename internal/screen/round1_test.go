package screen

import (
	"image"
	"testing"
	"time"
)

func scene(w, h int, bg uint8, boxes ...struct {
	r image.Rectangle
	v uint8
}) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := bg
			for _, b := range boxes {
				if image.Pt(x, y).In(b.r) {
					v = b.v
				}
			}
			i := img.PixOffset(x, y)
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = v, v, v, 255
		}
	}
	return img
}

type box = struct {
	r image.Rectangle
	v uint8
}

// I1: большая панель рядом с курсором не перебивает тултип у курсора.
func TestFindTooltipPrefersCursorOverBigPanel(t *testing.T) {
	tip := image.Rect(240, 190, 460, 270)
	img := scene(600, 454, 70, box{image.Rect(330, 20, 590, 180), 30}, box{tip, 18})
	r, ok := FindTooltip(img)
	if !ok || !tip.In(r) || !r.In(tip.Inset(-CropMargin-2)) {
		t.Fatalf("%v %v", r, ok)
	}
	// Тултип сбоку от курсора, а светлая панель крупнее и тоже рядом: тёмная
	// (тултип) важнее.
	tip = image.Rect(305, 120, 520, 200)
	img = scene(600, 454, 70, box{image.Rect(20, 150, 296, 300), 140}, box{tip, 18})
	r, ok = FindTooltip(img)
	if !ok || !tip.In(r) || !r.In(tip.Inset(-CropMargin-2)) {
		t.Fatalf("тёмная панель: %v %v", r, ok)
	}
}

// M3: полосатая сцена на 4K — поиск ограничен по времени.
func TestFindTooltipFastOnBusy4K(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1188, 907))
	for y := 0; y < 907; y++ {
		for x := 0; x < 1188; x++ {
			v := uint8(40)
			if y%4 < 2 {
				v = 120
			}
			i := img.PixOffset(x, y)
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = v, v, v, 255
		}
	}
	t0 := time.Now()
	FindTooltip(img)
	if d := time.Since(t0); d > 60*time.Millisecond {
		t.Errorf("4K полосы: %v", d)
	}
	// И тултип на 4K находится (в координатах исходного снимка).
	tip := image.Rect(500, 380, 940, 540)
	r, ok := FindTooltip(scene(1188, 907, 70, box{tip, 18}))
	if !ok || !tip.In(r) || !r.In(tip.Inset(-CropMargin-4)) {
		t.Errorf("4K тултип: %v %v", r, ok)
	}
}
