package ocr

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"albionzonefix/internal/paddle"
)

// Reader — своё распознавание строк (paddle.Engine; в тестах — подделка).
type Reader interface {
	Load(model string) error
	Read(img *image.RGBA, model string) ([]paddle.Text, error)
	Version() string
}

// Combined — распознавание для карточки зоны: сначала своё (PaddleOCR
// через ONNX Runtime, internal/paddle) по картинке в исходном разрешении,
// запасное — Windows OCR, как раньше: нет onnxruntime.dll или модели,
// ошибка ONNX Runtime, пустой итог, вариант картинки (серый, инверсия,
// порог — их делают для Windows OCR), вся рамка на светлом фоне.
type Combined struct {
	Dir     string // папка ocr: onnxruntime.dll и модели
	Threads int    // потоков ONNX Runtime
	// Native — картинка в исходном разрешении для записанного снимка
	// (screen.Native); cropped — обрезана по тултипу.
	Native  func(path string) (img *image.RGBA, cropped bool, err error)
	Windows func(ctx context.Context, path string, langs []string) (map[string][]string, error)
	Logf    func(string, ...any)
	// Open — открыть движок (nil — paddle.Open).
	Open func(dir string, threads int) (Reader, error)

	once    sync.Once
	eng     Reader
	loadErr error
	broken  string // своё распознавание сломалось (паника) — больше не зовём

	mu        sync.Mutex
	lastImg   *image.RGBA // последняя распознанная картинка и её строки:
	lastLines []string    // второй язык не распознаёт её заново
}

// Model — модель распознавания: восточнославянская читает и кириллицу, и
// латиницу (английский клиент тоже).
const Model = paddle.ModelEslav

// Files — есть ли файлы своего распознавания (библиотека и модель).
func (c *Combined) Files() bool {
	if c == nil || c.Dir == "" || paddle.LibName == "" {
		return false
	}
	for _, f := range []string{paddle.LibName, Model} {
		if _, err := os.Stat(filepath.Join(c.Dir, f)); err != nil {
			return false
		}
	}
	return true
}

// Warm загружает движок и модель (при запуске, в горутине): первое нажатие
// не ждёт.
func (c *Combined) Warm() { c.engine() }

func (c *Combined) engine() (Reader, error) {
	c.once.Do(func() {
		defer func() {
			if v := recover(); v != nil {
				c.eng, c.loadErr = nil, fmt.Errorf("сбой своего распознавания при загрузке: %v", v)
				c.logf("OCR: %v\n%s", c.loadErr, debug.Stack())
			}
		}()
		if !c.Files() {
			c.loadErr = fmt.Errorf("нет %s или модели в %s", paddle.LibName, c.Dir)
			c.logf("OCR: своё распознавание не загружено: %v", c.loadErr)
			return
		}
		open := c.Open
		if open == nil {
			open = func(dir string, threads int) (Reader, error) { return paddle.Open(dir, threads) }
		}
		t0 := time.Now()
		eng, err := open(c.Dir, max(c.Threads, 1))
		if err == nil {
			err = eng.Load(Model)
		}
		if err != nil {
			c.loadErr = err
			c.logf("OCR: своё распознавание не загружено: %v", err)
			return
		}
		c.eng = eng
		c.logf("OCR: своё распознавание готово (onnxruntime %s, %s) за %v", eng.Version(), Model, time.Since(t0).Round(time.Millisecond))
	})
	return c.eng, c.loadErr
}

func (c *Combined) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}

// Hint — как ocr.Hint, но с файлами своего распознавания языки Windows OCR
// не нужны (оно читает и русский, и английский).
func (c *Combined) Hint(installed []string) string {
	if c.Files() {
		return ""
	}
	return Hint(installed)
}

// Pick — как ocr.Pick; с файлами своего распознавания и без русского и
// английского OCR Windows — всё равно оба языка (строки те же).
func (c *Combined) Pick(installed []string) []string {
	p := Pick(installed)
	if len(p) == 0 && c.Files() {
		return []string{"ru-RU", "en-US"}
	}
	return p
}

// errFallback — почему Windows OCR.
type errFallback struct{ why string }

func (e errFallback) Error() string { return e.why }

// Recognize — как ocr.Recognize: язык → строки. Своё распознавание языка
// не различает — одни и те же строки на всех langs.
func (c *Combined) Recognize(ctx context.Context, path string, langs []string) (map[string][]string, error) {
	lines, took, err := c.read(path)
	if err != nil {
		var fb errFallback
		if errors.As(err, &fb) {
			c.logf("OCR: windows (запасной: %s)", fb.why)
		} else {
			c.logf("OCR: windows (запасной: %v)", err)
		}
		if c.Windows == nil {
			return nil, err
		}
		return c.Windows(ctx, path, langs)
	}
	if took >= 0 {
		c.logf("OCR: paddle, %d стр. за %v: %s", len(lines), took.Round(time.Millisecond), strings.Join(lines, " | "))
	}
	out := make(map[string][]string, len(langs))
	for _, l := range langs {
		out[l] = append([]string(nil), lines...)
	}
	return out, nil
}

// read — строки своим распознаванием; took < 0 — взяты из прошлого раза.
// Паника внутри (деление на строки, декод, размеры от модели) — запасной
// Windows OCR, а своё распознавание до перезапуска не зовётся.
func (c *Combined) read(path string) (lines []string, took time.Duration, err error) {
	defer func() {
		if v := recover(); v != nil {
			why := fmt.Sprintf("сбой своего распознавания: %v", v)
			c.logf("OCR: %s\n%s", why, debug.Stack())
			c.mu.Lock()
			c.broken = why
			c.mu.Unlock()
			lines, took, err = nil, 0, errFallback{why}
		}
	}()
	c.mu.Lock()
	broken := c.broken
	c.mu.Unlock()
	if broken != "" {
		return nil, 0, errFallback{broken}
	}
	eng, err := c.engine()
	if err != nil {
		return nil, 0, errFallback{"нет своего распознавания: " + err.Error()}
	}
	if c.Native == nil {
		return nil, 0, errFallback{"нет снимка в исходном разрешении"}
	}
	img, cropped, err := c.Native(path)
	if err != nil {
		return nil, 0, errFallback{"вариант картинки"}
	}
	if !cropped && !paddle.Dark(img) {
		return nil, 0, errFallback{"тултип не вырезан, фон не тёмный"}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.broken != "" {
		return nil, 0, errFallback{c.broken}
	}
	if img == c.lastImg && c.lastLines != nil {
		return c.lastLines, -1, nil
	}
	t0 := time.Now()
	ts, err := eng.Read(img, Model)
	took = time.Since(t0)
	if err != nil {
		return nil, 0, errFallback{"ошибка onnxruntime: " + err.Error()}
	}
	if len(ts) == 0 {
		return nil, 0, errFallback{"своё распознавание ничего не прочитало"}
	}
	c.lastImg, c.lastLines = img, paddle.Lines(ts)
	return c.lastLines, took, nil
}
