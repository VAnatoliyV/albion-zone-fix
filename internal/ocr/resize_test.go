package ocr

// resize — уменьшение картинки усреднением (как мельче текст на экране).
import "image"

func resize(src *image.RGBA, s float64) *image.RGBA {
	b := src.Bounds()
	w, h := int(float64(b.Dx())*s), int(float64(b.Dy())*s)
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			x0, x1 := int(float64(x)/s), int(float64(x+1)/s)
			y0, y1 := int(float64(y)/s), int(float64(y+1)/s)
			if x1 <= x0 {
				x1 = x0 + 1
			}
			if y1 <= y0 {
				y1 = y0 + 1
			}
			var acc [4]int
			n := 0
			for yy := y0; yy < y1 && yy < b.Dy(); yy++ {
				for xx := x0; xx < x1 && xx < b.Dx(); xx++ {
					o := src.PixOffset(xx, yy)
					for c := 0; c < 4; c++ {
						acc[c] += int(src.Pix[o+c])
					}
					n++
				}
			}
			o := out.PixOffset(x, y)
			for c := 0; c < 4; c++ {
				out.Pix[o+c] = uint8(acc[c] / max(n, 1))
			}
		}
	}
	return out
}
