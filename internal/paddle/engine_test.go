package paddle

import (
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Для проверок с ONNX Runtime нужны библиотека и модели в кэше сборки
// (~/.cache/albion-journal, их кладёт туда собрать.sh) или в AJ_OCR_LIB и
// AJ_OCR_MODELS. Нет — такие проверки пропускаются.
func testEngine(t testing.TB) *Engine {
	t.Helper()
	home, _ := os.UserHomeDir()
	lib := os.Getenv("AJ_OCR_LIB")
	if lib == "" {
		m, _ := filepath.Glob(filepath.Join(home, ".cache", "albion-journal", "ort", "onnxruntime-osx-*", "lib", LibName))
		if len(m) == 0 {
			t.Skip("нет libonnxruntime в ~/.cache/albion-journal/ort")
		}
		lib = m[len(m)-1]
	}
	dir := os.Getenv("AJ_OCR_MODELS")
	if dir == "" {
		dir = filepath.Join(home, ".cache", "albion-journal", "models")
	}
	if _, err := os.Stat(filepath.Join(dir, ModelEslav)); err != nil {
		t.Skip("нет модели " + ModelEslav)
	}
	en, err := OpenWith(lib, dir, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(en.Close)
	return en
}

func loadPNG(t testing.TB, path string) *image.RGBA {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	b := m.Bounds()
	img := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(img, img.Bounds(), m, b.Min, draw.Src)
	return img
}

// Native — снимок тестера в исходном разрешении (в файле он увеличен ×k).
func Native(t testing.TB, name string, k int) *image.RGBA {
	t.Helper()
	src := loadPNG(t, filepath.Join("..", "screen", "testdata", name))
	return Downscale(src, k)
}

// Downscale — уменьшение в k раз усреднением.
func Downscale(src *image.RGBA, k int) *image.RGBA {
	b := src.Bounds()
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

// contains — в каком-то из lines есть want.
func hasLine(lines []string, want string) bool {
	for _, l := range lines {
		if strings.Contains(l, want) {
			return true
		}
	}
	return false
}

// Настоящие снимки тестера в исходном разрешении (тултип ~215×70):
// заголовок, название и время читаются (Windows OCR здесь давал «лутьАволоно в»).
func TestReadRealTooltips(t *testing.T) {
	en := testEngine(t)
	for _, c := range []struct {
		file string
		want []string
	}{
		{"zone-capture-doubt.png", []string{"Авалона в", "Pasos-Avosam", "7 ч 05 м"}},
		{"zone-capture.png", []string{"Путь Ав", "Fleos-A", "6 ч 26 м"}},
	} {
		img := tooltipOf(t, c.file, 2)
		t0 := time.Now()
		ts, err := en.Read(img, ModelEslav)
		took := time.Since(t0)
		if err != nil {
			t.Fatal(err)
		}
		lines := Lines(ts)
		t.Logf("%s за %v: %q", c.file, took, lines)
		for _, w := range c.want {
			if !hasLine(lines, w) {
				t.Errorf("%s: нет %q в %q", c.file, w, lines)
			}
		}
	}
}

// Английский клиент: восточнославянская модель читает и его — отдельная
// английская не нужна. Снимков английского клиента нет: тултипы нарисованы
// (Arial, размеры как у игры на 1080p).
func TestReadEnglishSynthetic(t *testing.T) {
	en := testEngine(t)
	for file, want := range map[string][]string{
		"syn-en-1.png": {"Road of Avalon to", "Pasos-Avosam", "Closes in 6 h 25 m"},
		"syn-en-2.png": {"Unstable Roads to", "Fleos-Aluttum", "Closes for your party in 4 m 18 s"},
		"syn-ru-2.png": {"Нестабильные Пути в", "Fleos-Aluttum", "Закроется для вашей группы через 4 м 18 с"},
	} {
		ts, err := en.Read(loadPNG(t, filepath.Join("testdata", file)), ModelEslav)
		if err != nil {
			t.Fatal(err)
		}
		lines := Lines(ts)
		for _, w := range want {
			if !hasLine(lines, w) {
				t.Errorf("%s: нет %q в %q", file, w, lines)
			}
		}
	}
}

// Вся рамка без обрезки (карта под курсором): мусора много, но кусков не
// больше maxItems — распознавание не тянется секунду.
func TestReadFullFrame(t *testing.T) {
	en := testEngine(t)
	img := Native(t, "zone-capture.png", 2)
	t0 := time.Now()
	ts, err := en.Read(img, ModelEslav)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("вся рамка за %v: %q", time.Since(t0), Lines(ts))
	if !hasLine(Lines(ts), "6 ч 26 м") {
		t.Errorf("время не прочитано: %q", Lines(ts))
	}
}

func BenchmarkReadTooltip(b *testing.B) {
	en := testEngine(b)
	img := tooltipOf(b, "zone-capture-doubt.png", 2)
	en.Read(img, ModelEslav)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := en.Read(img, ModelEslav); err != nil {
			b.Fatal(err)
		}
	}
}

// После Close — ошибка, а не паника.
func TestClosedEngine(t *testing.T) {
	en := &Engine{s: map[string]*session{}}
	if err := en.Load(ModelEslav); err == nil {
		t.Error("закрытый движок загрузил модель")
	}
}
