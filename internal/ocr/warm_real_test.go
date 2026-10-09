package ocr

import (
	"os"
	"path/filepath"
	"testing"

	"albionzonefix/internal/paddle"
)

// Пробная картинка прогрева на настоящем движке: деление на строки находит
// в ней текст, значит модель действительно запускается (а не отсеивается
// пустой картинкой).
func TestWarmImageReachesModel(t *testing.T) {
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
	eng, err := paddle.Open(dir, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Load(Model); err != nil {
		t.Fatal(err)
	}
	ts, err := eng.Read(warmImage(), Model)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) == 0 {
		t.Fatal("пробная картинка не дошла до модели: строк 0")
	}
}
