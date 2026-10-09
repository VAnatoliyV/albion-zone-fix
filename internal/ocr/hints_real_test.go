package ocr

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"albionzonefix/internal/paddle"
	"albionzonefix/internal/screen"
	"albionzonefix/internal/zonecard"
)

// Мелкий текст (снимки тестера, уменьшенные как на экране меньше 1080p):
// подсказки справочником находят время на 70% и уверенную зону на 60%,
// где побуквенное чтение их теряет.
func TestHintsSmallText(t *testing.T) {
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
	d := zonecard.Default()
	for _, c := range []struct {
		file  string
		scale float64
		zone  string
		left  time.Duration // 0 — время не требуем
	}{
		{"zone-capture-doubt.png", 0.7, "Pasos-Avosam", 7*time.Hour + 5*time.Minute},
		{"zone-capture.png", 0.7, "Fieos-Aiuttum", 6*time.Hour + 26*time.Minute},
		{"zone-capture.png", 0.6, "Fieos-Aiuttum", 0},
		{"zone-capture.png", 1, "Fieos-Aiuttum", 6*time.Hour + 26*time.Minute},
	} {
		raw := resize(downscale(t, filepath.Join("..", "screen", "testdata", c.file), 2), c.scale)
		box, ok := screen.FindTooltip(raw)
		if !ok {
			t.Fatalf("%s ×%.1f: тултип не найден", c.file, c.scale)
		}
		path := filepath.Join(t.TempDir(), "zone-capture.png")
		screen.RememberCrop(path, raw, box, 4)
		cb := &Combined{Dir: dir, Threads: 2, Native: screen.Native,
			Windows: func(context.Context, string, []string) (map[string][]string, error) {
				return nil, errors.New("не нужен")
			},
			Hints: &Hints{Names: d.PortalNames(), TimeRunes: "0123456789чмсдhmsd "}}
		langs := []string{"ru-RU", "en-US"}
		byLang, err := cb.Recognize(context.Background(), path, langs)
		if err != nil {
			t.Fatal(err)
		}
		res, err := zonecard.Choose(d, byLang, langs, time.Now())
		if err != nil {
			t.Fatalf("%s ×%.1f: %v", c.file, c.scale, err)
		}
		if z := res.Zone(); z.Name != c.zone || res.Doubtful() {
			t.Errorf("%s ×%.1f: зона %s, сомнительно %v", c.file, c.scale, z.Name, res.Doubtful())
		}
		if c.left > 0 && res.Tooltip.Left != c.left {
			t.Errorf("%s ×%.1f: время %v, ждали %v", c.file, c.scale, res.Tooltip.Left, c.left)
		}
		if c.scale == 1 && res.Tooltip.TimeLoose {
			t.Errorf("%s: на обычном размере время должно быть строгим", c.file)
		}
	}
}
