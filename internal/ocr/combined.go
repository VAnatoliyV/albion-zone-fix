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
	"sync/atomic"
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
	// Unavailable — своё распознавание не загрузилось или сломалось
	// (один раз): пора заново проверить языки Windows OCR — подсказка
	// «установите язык» снова нужна.
	Unavailable func()
	// LoadWait — сколько нажатие ждёт загрузки модели (0 — LoadWaitDefault),
	// ReadTimeout — распознавания (0 — ReadTimeoutDefault). Дольше —
	// запасной Windows OCR: зависание не держит кнопку.
	LoadWait, ReadTimeout time.Duration

	start   sync.Once
	started atomic.Bool
	mkCh    sync.Once
	loaded  chan struct{} // закрыт — загрузка кончилась (eng или loadErr)
	eng     Reader
	loadErr error
	unavail sync.Once
	needVC  atomic.Bool // не загрузилось: нет VC++ runtime

	mu        sync.Mutex
	broken    string      // своё распознавание сломалось (паника) — больше не зовём
	busy      bool        // идёт распознавание (опоздавшее или прогрев) — второе в движок не шлём
	lastImg   *image.RGBA // последняя распознанная картинка и её строки:
	lastLines []string    // второй язык не распознаёт её заново
}

// VCRuntimeText — подсказка в журнал (на странице — ocr.hint.vcRuntime).
const VCRuntimeText = "Для точного распознавания установите Microsoft Visual C++ Redistributable (x64) с сайта Microsoft: " +
	paddle.VCRedistURL + " — пока работает распознавание Windows"

// NeedVCRuntime — своё распознавание не загрузилось из-за отсутствия
// Microsoft Visual C++ Redistributable (x64).
func (c *Combined) NeedVCRuntime() bool { return c.needVC.Load() }

// Сроки по умолчанию: загрузка обычно ~50 мс, распознавание тултипа ~0.1–0.3 с.
const (
	LoadWaitDefault    = 3 * time.Second
	ReadTimeoutDefault = 2 * time.Second
)

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

// Warm загружает движок и модель и прогоняет пробную картинку (при
// запуске, в горутине): первый прогон ONNX Runtime самый долгий — пусть он
// случится здесь, а не на первом нажатии.
func (c *Combined) Warm() {
	<-c.begin()
	if c.eng == nil {
		return
	}
	c.mu.Lock()
	if c.busy || c.broken != "" {
		c.mu.Unlock()
		return
	}
	c.busy = true
	c.mu.Unlock()
	t0 := time.Now()
	r := c.run(c.eng, warmImage())
	c.mu.Lock()
	c.busy = false
	c.mu.Unlock()
	if r.bad != nil {
		c.disable(fmt.Sprintf("сбой своего распознавания при прогреве: %v", r.bad))
		return
	}
	c.logf("OCR: прогрев своего распознавания за %v", time.Since(t0).Round(time.Millisecond))
}

// warmImage — тёмная полоса со светлыми штрихами, похожая на строку
// тултипа: на ней деление на строки находит текст и модель реально
// запускается.
func warmImage() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 200, 40))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 20, 20, 24, 255
	}
	for x := 10; x < 190; x++ {
		if x%7 > 2 {
			for y := 12; y < 28; y++ {
				o := img.PixOffset(x, y)
				img.Pix[o], img.Pix[o+1], img.Pix[o+2] = 230, 230, 230
			}
		}
	}
	return img
}

// readResult — итог одного распознавания; bad — паника внутри.
type readResult struct {
	ts  []paddle.Text
	err error
	bad any
}

// run — одно распознавание с перехватом паники.
func (c *Combined) run(eng Reader, img *image.RGBA) (r readResult) {
	defer func() {
		if v := recover(); v != nil {
			r = readResult{bad: fmt.Sprintf("%v\n%s", v, debug.Stack())}
		}
	}()
	r.ts, r.err = eng.Read(img, Model)
	return r
}

// begin запускает загрузку (один раз, в своей горутине) и отдаёт канал её
// конца.
func (c *Combined) begin() chan struct{} {
	done := c.done()
	c.start.Do(func() {
		c.started.Store(true)
		go func() {
			defer close(c.loaded)
			c.load()
			if c.loadErr != nil {
				c.markUnavailable()
			}
		}()
	})
	return done
}

func (c *Combined) done() chan struct{} {
	c.mkCh.Do(func() { c.loaded = make(chan struct{}) })
	return c.loaded
}

func (c *Combined) load() {
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
		if errors.Is(err, paddle.ErrNoVCRuntime) {
			c.needVC.Store(true)
			c.logf("OCR: %s", VCRuntimeText)
		}
		return
	}
	c.eng = eng
	c.logf("OCR: своё распознавание готово (onnxruntime %s, %s) за %v", eng.Version(), Model, time.Since(t0).Round(time.Millisecond))
}

// engine — движок, если загрузка кончилась за wait (запускает её, если ещё
// нет). ok=false — ещё загружается.
func (c *Combined) engine(wait time.Duration) (eng Reader, err error, ok bool) {
	done := c.begin()
	select {
	case <-done:
		return c.eng, c.loadErr, true
	case <-time.After(wait):
		return nil, nil, false
	}
}

func (c *Combined) markUnavailable() {
	c.unavail.Do(func() {
		if c.Unavailable != nil {
			c.Unavailable()
		}
	})
}

// usable — своё распознавание есть или может появиться: загрузилось; или
// ещё не загружалось (тогда — есть ли файлы). Не ждёт.
func (c *Combined) usable() bool {
	c.mu.Lock()
	broken := c.broken != ""
	c.mu.Unlock()
	if broken {
		return false
	}
	if !c.started.Load() {
		return c.Files()
	}
	select {
	case <-c.done():
		return c.eng != nil
	default:
		return c.Files() // загружается
	}
}

func (c *Combined) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}

// Hint — как ocr.Hint, но пока своё распознавание есть, языки Windows OCR
// не нужны (оно читает и русский, и английский). Не загрузилось или
// сломалось — обычная подсказка (Unavailable зовёт проверку заново).
func (c *Combined) Hint(installed []string) string {
	if c.usable() {
		return ""
	}
	return Hint(installed)
}

// Pick — как ocr.Pick; пока своё распознавание есть, а русского и
// английского OCR Windows нет — всё равно оба языка (строки те же).
func (c *Combined) Pick(installed []string) []string {
	p := Pick(installed)
	if len(p) == 0 && c.usable() {
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
// Паника внутри (деление на строки, декод, размеры от модели) или
// зависание — запасной Windows OCR, а своё распознавание до перезапуска
// не зовётся.
func (c *Combined) read(path string) (lines []string, took time.Duration, err error) {
	defer func() {
		if v := recover(); v != nil {
			why := fmt.Sprintf("сбой своего распознавания: %v", v)
			c.logf("OCR: %s\n%s", why, debug.Stack())
			c.disable(why)
			lines, took, err = nil, 0, errFallback{why}
		}
	}()
	c.mu.Lock()
	broken, busy := c.broken, c.busy
	c.mu.Unlock()
	if broken != "" {
		return nil, 0, errFallback{broken}
	}
	if busy {
		return nil, 0, errFallback{"своё распознавание ещё занято прошлым снимком"}
	}
	eng, err, ok := c.engine(orDefault(c.LoadWait, LoadWaitDefault))
	if !ok {
		return nil, 0, errFallback{"своё распознавание ещё загружается"}
	}
	if err != nil {
		return nil, 0, errFallback{"нет своего распознавания: " + err.Error()}
	}
	if c.Native == nil {
		return nil, 0, errFallback{"нет снимка в исходном разрешении"}
	}
	img, cropped, err := c.Native(path)
	if err != nil {
		return nil, 0, errFallback{"вариант картинки или не последний снимок: " + err.Error()}
	}
	if !cropped && !paddle.Dark(img) {
		return nil, 0, errFallback{"тултип не вырезан, фон не тёмный"}
	}
	c.mu.Lock()
	if img == c.lastImg && c.lastLines != nil {
		lines := c.lastLines
		c.mu.Unlock()
		return lines, -1, nil
	}
	c.mu.Unlock()
	// Распознавание — в своей горутине со сроком: долгое не держит нажатие.
	// Опоздавшее не выключает своё распознавание: пока оно думает — запасное,
	// ответило — снова своё (первый прогон на слабом процессоре бывает
	// дольше срока). Выключаем только при сбое или если так и не ответило.
	c.mu.Lock()
	if c.busy {
		c.mu.Unlock()
		return nil, 0, errFallback{"своё распознавание ещё занято прошлым снимком"}
	}
	c.busy = true
	c.mu.Unlock()
	ch := make(chan readResult, 1)
	t0 := time.Now()
	go func() { ch <- c.run(eng, img) }()
	limit := orDefault(c.ReadTimeout, ReadTimeoutDefault)
	var r readResult
	select {
	case r = <-ch:
		c.mu.Lock()
		c.busy = false
		c.mu.Unlock()
	case <-time.After(limit):
		why := fmt.Sprintf("своё распознавание не ответило за %v", limit)
		go c.late(ch, t0)
		return nil, 0, errFallback{why}
	}
	took = time.Since(t0)
	if r.bad != nil {
		why := fmt.Sprintf("сбой своего распознавания: %v", r.bad)
		c.disable(why)
		return nil, 0, errFallback{why}
	}
	if r.err != nil {
		return nil, 0, errFallback{"ошибка onnxruntime: " + r.err.Error()}
	}
	if len(r.ts) == 0 {
		return nil, 0, errFallback{"своё распознавание ничего не прочитало"}
	}
	lines = paddle.Lines(r.ts)
	c.mu.Lock()
	c.lastImg, c.lastLines = img, lines
	c.mu.Unlock()
	return lines, took, nil
}

// late ждёт опоздавшее распознавание: ответило — своё снова в деле,
// сбой — выключаем до перезапуска; не ответило за LateLimit — тоже.
func (c *Combined) late(ch chan readResult, t0 time.Time) {
	select {
	case r := <-ch:
		c.mu.Lock()
		c.busy = false
		c.mu.Unlock()
		if r.bad != nil {
			c.disable(fmt.Sprintf("сбой своего распознавания: %v", r.bad))
			return
		}
		c.logf("OCR: своё распознавание ответило через %v — снова включено", time.Since(t0).Round(time.Millisecond))
	case <-time.After(LateLimit):
		c.disable(fmt.Sprintf("своё распознавание не ответило и за %v", LateLimit))
	}
}

// LateLimit — сколько ждём опоздавшее распознавание, прежде чем счесть его
// зависшим.
var LateLimit = 30 * time.Second

// disable — своё распознавание больше не звать (до перезапуска).
func (c *Combined) disable(why string) {
	c.mu.Lock()
	first := c.broken == ""
	if first {
		c.broken = why
	}
	c.mu.Unlock()
	if first {
		c.logf("OCR: своё распознавание выключено до перезапуска: %s", why)
		c.markUnavailable()
	}
}

func orDefault(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}
