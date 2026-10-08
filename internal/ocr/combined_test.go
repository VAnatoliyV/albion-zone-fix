package ocr

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"albionzonefix/internal/paddle"
	"albionzonefix/internal/screen"
	"albionzonefix/internal/zonecard"
)

type fakeReader struct {
	lines []string
	err   error
	reads int
}

func (f *fakeReader) Load(string) error { return nil }
func (f *fakeReader) Version() string   { return "fake" }
func (f *fakeReader) Read(*image.RGBA, string) ([]paddle.Text, error) {
	f.reads++
	var ts []paddle.Text
	for _, l := range f.lines {
		ts = append(ts, paddle.Text{Text: l, Score: 1})
	}
	return ts, f.err
}

// fakeDir — папка с пустыми файлами библиотеки и модели (Files = true).
func fakeDir(t *testing.T) string {
	dir := t.TempDir()
	for _, f := range []string{paddle.LibName, Model} {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func darkImg() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	return img
}

func TestCombinedFallback(t *testing.T) {
	winLines := map[string][]string{"ru-RU": {"windows"}}
	img := darkImg()
	light := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for i := range light.Pix {
		light.Pix[i] = 220
	}
	for _, c := range []struct {
		name    string
		files   bool
		reader  *fakeReader
		openErr error
		native  func(string) (*image.RGBA, bool, error)
		paddle  bool   // ответ своего распознавания
		why     string // в журнале — почему запасной
	}{
		{name: "своё", files: true, reader: &fakeReader{lines: []string{"Путь Авалона в"}}, paddle: true},
		{name: "нет файлов", files: false, reader: &fakeReader{lines: []string{"x"}}, why: "нет своего распознавания"},
		{name: "не загрузилось", files: true, openErr: errors.New("не загрузилась onnxruntime.dll"), why: "не загрузилась"},
		{name: "ошибка ORT", files: true, reader: &fakeReader{err: errors.New("Run: bad")}, why: "ошибка onnxruntime"},
		{name: "пусто", files: true, reader: &fakeReader{}, why: "ничего не прочитало"},
		{name: "вариант", files: true, reader: &fakeReader{lines: []string{"x"}},
			native: func(string) (*image.RGBA, bool, error) { return nil, false, screen.ErrNoNative }, why: "вариант картинки"},
		{name: "вся рамка светлая", files: true, reader: &fakeReader{lines: []string{"x"}},
			native: func(string) (*image.RGBA, bool, error) { return light, false, nil }, why: "фон не тёмный"},
		{name: "вся рамка тёмная", files: true, reader: &fakeReader{lines: []string{"x"}},
			native: func(string) (*image.RGBA, bool, error) { return img, false, nil }, paddle: true},
	} {
		dir := t.TempDir()
		if c.files {
			dir = fakeDir(t)
		}
		var log []string
		native := c.native
		if native == nil {
			native = func(string) (*image.RGBA, bool, error) { return img, true, nil }
		}
		winCalls := 0
		cb := &Combined{
			Dir: dir, Native: native,
			Windows: func(ctx context.Context, path string, langs []string) (map[string][]string, error) {
				winCalls++
				return winLines, nil
			},
			Logf: func(f string, a ...any) { log = append(log, strings.TrimSpace(fmtS(f, a...))) },
			Open: func(string, int) (Reader, error) {
				if c.openErr != nil {
					return nil, c.openErr
				}
				return c.reader, nil
			},
		}
		got, err := cb.Recognize(context.Background(), "zone-capture.png", []string{"ru-RU", "en-US"})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		all := strings.Join(log, "\n")
		if c.paddle {
			if winCalls != 0 || len(got["ru-RU"]) == 0 || len(got["en-US"]) == 0 || got["ru-RU"][0] == "windows" {
				t.Errorf("%s: ждали своё на обоих языках: %v", c.name, got)
			}
			if !strings.Contains(all, "OCR: paddle") {
				t.Errorf("%s: в журнале %q", c.name, all)
			}
			continue
		}
		if winCalls != 1 || got["ru-RU"][0] != "windows" {
			t.Errorf("%s: ждали Windows OCR: %v", c.name, got)
		}
		if !strings.Contains(all, "OCR: windows (запасной: ") || !strings.Contains(all, c.why) {
			t.Errorf("%s: в журнале %q, ждали %q", c.name, all, c.why)
		}
	}
}

// Второй язык (LangPlan) по той же картинке не распознаётся заново.
func TestCombinedCache(t *testing.T) {
	r := &fakeReader{lines: []string{"a"}}
	img := darkImg()
	cb := &Combined{Dir: fakeDir(t), Open: func(string, int) (Reader, error) { return r, nil },
		Native: func(string) (*image.RGBA, bool, error) { return img, true, nil }}
	cb.Recognize(context.Background(), "p", []string{"ru-RU"})
	got, _ := cb.Recognize(context.Background(), "p", []string{"en-US"})
	if r.reads != 1 || len(got["en-US"]) != 1 {
		t.Errorf("прочитано %d раз, %v", r.reads, got)
	}
}

func TestCombinedHintPick(t *testing.T) {
	without := &Combined{Dir: t.TempDir()}
	with := &Combined{Dir: fakeDir(t)}
	if without.Hint(nil) != HintNone || with.Hint(nil) != "" {
		t.Errorf("подсказки %q %q", without.Hint(nil), with.Hint(nil))
	}
	if p := with.Pick([]string{"de-DE"}); len(p) != 2 {
		t.Errorf("языки со своим распознаванием: %v", p)
	}
	if p := without.Pick([]string{"de-DE"}); len(p) != 0 {
		t.Errorf("языки без своего: %v", p)
	}
}

func fmtS(f string, a ...any) string { return fmt.Sprintf(f, a...) }

// Весь путь на настоящем снимке тестера: исходная рамка → обрезка по
// тултипу (как при снимке) → своё распознавание → опознание карточки:
// портал дорог в Pasos-Avosam, закроется через 7 ч 05 м.
func TestCombinedRealCard(t *testing.T) {
	home, _ := os.UserHomeDir()
	libs, _ := filepath.Glob(filepath.Join(home, ".cache", "albion-journal", "ort", "onnxruntime-osx-*", "lib", paddle.LibName))
	model := filepath.Join(home, ".cache", "albion-journal", "models", Model)
	if len(libs) == 0 || paddle.LibName == "" {
		t.Skip("нет libonnxruntime в ~/.cache/albion-journal/ort")
	}
	if _, err := os.Stat(model); err != nil {
		t.Skip("нет модели")
	}
	dir := t.TempDir()
	for src, name := range map[string]string{libs[len(libs)-1]: paddle.LibName, model: Model} {
		if err := os.Symlink(src, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	cb := &Combined{Dir: dir, Threads: 2, Native: screen.Native,
		Windows: func(context.Context, string, []string) (map[string][]string, error) {
			return nil, errors.New("Windows OCR не должен понадобиться")
		}}
	d := zonecard.Default()
	for _, c := range []struct {
		file string
		zone string
		left time.Duration
	}{
		{"zone-capture-doubt.png", "Pasos-Avosam", 7*time.Hour + 5*time.Minute},
		{"zone-capture.png", "Fieos-Aiuttum", 6*time.Hour + 26*time.Minute},
	} {
		raw := downscale(t, filepath.Join("..", "screen", "testdata", c.file), 2)
		box, ok := screen.FindTooltip(raw)
		if !ok {
			t.Fatal("тултип не найден")
		}
		path := filepath.Join(t.TempDir(), "zone-capture.png")
		screen.RememberCrop(path, raw, box, 4)
		t0 := time.Now()
		langs := []string{"ru-RU", "en-US"}
		byLang, err := cb.Recognize(context.Background(), path, langs)
		if err != nil {
			t.Fatal(err)
		}
		took := time.Since(t0)
		res, err := zonecard.Choose(d, byLang, langs, time.Now())
		if err != nil {
			t.Fatalf("%s: %v (%q)", c.file, err, byLang["ru-RU"])
		}
		z := res.Zone()
		t.Logf("%s за %v: %q → %s, %v", c.file, took, byLang["ru-RU"], z.Name, res.Tooltip.Left)
		if !res.Portal || z.Name != c.zone || res.Tooltip.Left != c.left || res.Doubtful() || res.Tooltip.TimeLoose {
			t.Errorf("%s: портал %v, зона %s, время %v (нестрого %v), сомнительно %v", c.file,
				res.Portal, z.Name, res.Tooltip.Left, res.Tooltip.TimeLoose, res.Doubtful())
		}
	}
}

func downscale(t *testing.T, path string, k int) *image.RGBA {
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
