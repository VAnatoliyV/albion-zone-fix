// Пакет paddle — своё распознавание текста тултипа: модели распознавания
// строк PaddleOCR PP-OCRv5 (Apache-2.0, в ONNX от RapidAI/RapidOCR) через
// ONNX Runtime (MIT, onnxruntime.dll рядом с программой). Детектора нет:
// тултип уже вырезан (internal/screen), строки делит Split. Препроцесс и
// CTC-декод — как в RapidOCR (ch_ppocr_rec): высота 48, ширина по
// пропорции, (x/255 − 0.5)/0.5, каналы BGR, жадный декод.
//
// Windows OCR (internal/ocr) остаётся запасным: нет библиотеки или моделей,
// ошибка ONNX Runtime, пустой итог.
package paddle

import (
	"errors"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

// Height — высота строки на входе модели; MinWidth — ширина пачки не меньше
// (rec_img_shape [3, 48, 320] у RapidOCR); MaxWidth — шире строку сжимаем.
const (
	Height   = 48
	MinWidth = 320
	MaxWidth = 2048
)

// maxItems — кусков строк на картинку, не больше.
const maxItems = 16

// batch — строк в одном прогоне модели, не больше.
var batch = 4

// Модели распознавания (файлы в папке ocr).
const (
	// ModelEslav — восточнославянская: кириллица и латиница (русский и
	// английский клиент игры, названия зон латиницей).
	ModelEslav = "eslav_PP-OCRv5_rec_mobile.onnx"
)

// Charset — словарь модели из метаданных («character», символ на строку):
// 0 — пусто (blank CTC), потом символы, в конце пробел (как CTCLabelDecode).
func Charset(meta string) []string {
	meta = strings.ReplaceAll(meta, "\r\n", "\n")
	lines := strings.Split(meta, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	if len(lines) == 0 {
		return nil
	}
	out := make([]string, 0, len(lines)+2)
	out = append(out, "")
	out = append(out, lines...)
	return append(out, " ")
}

// Decode — жадный CTC-декод одной строки: probs — T шагов по C символов.
// Повторы подряд схлопываются, пустой (0) выкидывается; оценка — средняя
// вероятность оставленных символов.
func Decode(probs []float32, T, C int, chars []string) (string, float64) {
	var sb strings.Builder
	sum, n := 0.0, 0
	prev := -1
	for t := 0; t < T; t++ {
		row := probs[t*C : (t+1)*C]
		best, p := 0, row[0]
		for c := 1; c < C; c++ {
			if row[c] > p {
				best, p = c, row[c]
			}
		}
		if best != 0 && best != prev && best < len(chars) {
			sb.WriteString(chars[best])
			sum += float64(p)
			n++
		}
		prev = best
	}
	if n == 0 {
		return "", 0
	}
	return sb.String(), sum / float64(n)
}

// Prepare пишет полоску r картинки img в dst (3×48×W, BGR, нормировано) —
// как resize_norm_img у RapidOCR: высота 48, ширина по пропорции (не
// больше W), билинейно как cv2.resize, справа нули. Ответ — ширина
// полоски.
func Prepare(img *image.RGBA, r image.Rectangle, W int, dst []float32) int {
	w, h := r.Dx(), r.Dy()
	rw := min(W, int(math.Ceil(Height*float64(w)/float64(h))))
	rw = max(rw, 1)
	for i := range dst[:3*Height*W] {
		dst[i] = 0
	}
	sx, sy := float64(w)/float64(rw), float64(h)/float64(Height)
	type tap struct {
		i0, i1 int
		f      float64
	}
	taps := func(n, size int, scale float64) []tap {
		out := make([]tap, n)
		for d := 0; d < n; d++ {
			f := (float64(d)+0.5)*scale - 0.5
			s := int(math.Floor(f))
			f -= float64(s)
			if s < 0 {
				s, f = 0, 0
			}
			if s >= size-1 {
				s, f = size-1, 0
			}
			out[d] = tap{s, min(s+1, size-1), f}
		}
		return out
	}
	xs, ys := taps(rw, w, sx), taps(Height, h, sy)
	plane := Height * W
	for y, ty := range ys {
		r0 := img.Pix[img.PixOffset(r.Min.X, r.Min.Y+ty.i0):]
		r1 := img.Pix[img.PixOffset(r.Min.X, r.Min.Y+ty.i1):]
		for x, tx := range xs {
			for c := 0; c < 3; c++ {
				a := float64(r0[4*tx.i0+c])*(1-tx.f) + float64(r0[4*tx.i1+c])*tx.f
				b := float64(r1[4*tx.i0+c])*(1-tx.f) + float64(r1[4*tx.i1+c])*tx.f
				v := a*(1-ty.f) + b*ty.f
				// RGBA → BGR: канал c картинки идёт в плоскость 2−c.
				dst[(2-c)*plane+y*W+x] = float32(v/255*2 - 1)
			}
		}
	}
	return rw
}

// Text — распознанная строка.
type Text struct {
	Text  string
	Score float64 // средняя уверенность кусков
	Box   image.Rectangle
	// Segs — куски строки с выходом модели: по ним можно спросить, насколько
	// картинка похожа на известное слово (ocr.Hints), а не только взять
	// самую вероятную букву на каждом шаге.
	Segs []Seg
}

// Seg — кусок строки: текст жадного декода и вероятности символов
// (T шагов по C символов словаря Chars).
type Seg struct {
	Text  string
	Probs []float32
	T, C  int
}

// Engine — ONNX Runtime и загруженные модели. Одна на всю программу.
type Engine struct {
	mu      sync.Mutex
	dir     string
	threads int
	o       *ort
	e       *env
	s       map[string]*session
}

// Open грузит ONNX Runtime из папки dir (LibName рядом с моделями).
// threads — потоков на модель.
func Open(dir string, threads int) (*Engine, error) {
	if LibName == "" {
		return nil, errors.New("onnxruntime здесь не поддерживается")
	}
	return OpenWith(filepath.Join(dir, LibName), dir, threads)
}

// OpenWith — библиотека lib и модели в папке dir.
func OpenWith(lib, dir string, threads int) (*Engine, error) {
	if _, err := os.Stat(lib); err != nil {
		return nil, fmt.Errorf("нет %s", filepath.Base(lib))
	}
	o, err := loadORT(lib)
	if err != nil {
		return nil, err
	}
	e, err := o.newEnv()
	if err != nil {
		return nil, err
	}
	return &Engine{dir: dir, threads: max(threads, 1), o: o, e: e, s: map[string]*session{}}, nil
}

// Version — версия ONNX Runtime.
func (en *Engine) Version() string { return en.o.Version }

// Load загружает модель (файл в папке движка), если ещё не загружена.
func (en *Engine) Load(model string) error {
	en.mu.Lock()
	defer en.mu.Unlock()
	_, err := en.load(model)
	return err
}

func (en *Engine) load(model string) (*session, error) {
	if en.e == nil {
		return nil, errors.New("своё распознавание закрыто")
	}
	if s := en.s[model]; s != nil {
		return s, nil
	}
	b, err := os.ReadFile(filepath.Join(en.dir, model))
	if err != nil {
		return nil, fmt.Errorf("нет модели %s", model)
	}
	s, err := en.e.newSession(b, en.threads)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", model, err)
	}
	en.s[model] = s
	return s, nil
}

// Close освобождает модели и ONNX Runtime (саму библиотеку не выгружаем).
func (en *Engine) Close() {
	en.mu.Lock()
	defer en.mu.Unlock()
	for k, s := range en.s {
		s.close()
		delete(en.s, k)
	}
	if en.e != nil {
		en.e.close()
		en.e = nil
	}
}

// Мусор вместо значка: кусок строки с низкой уверенностью.
const (
	junkScore      = 0.35 // ниже — не текст
	junkShortScore = 0.6  // кусок в 1–2 символа ниже этого — значок
)

// Read делит картинку на строки (Split) и распознаёт их моделью model.
// Куски строки — каждый отдельно, текст собирается обратно через пробел;
// куски-значки (низкая уверенность) выкидываются.
func (en *Engine) Read(img *image.RGBA, model string) ([]Text, error) {
	lines := Split(img)
	if len(lines) == 0 {
		return nil, nil
	}
	b := img.Bounds()
	type item struct {
		line  int
		r     image.Rectangle
		ratio float64
		text  string
		score float64
		probs []float32
		T, C  int
	}
	var items []*item
	for i, ln := range lines {
		top, bot := b.Min.Y, b.Max.Y
		if i > 0 {
			top = b.Min.Y + lines[i-1].Box.Max.Y
		}
		if i+1 < len(lines) {
			bot = b.Min.Y + lines[i+1].Box.Min.Y
		}
		for _, sg := range ln.Segs {
			pad := max(2, sg.Dy()/4)
			r := sg.Add(b.Min).Inset(-pad).Intersect(b)
			r.Min.Y, r.Max.Y = max(r.Min.Y, top), min(r.Max.Y, bot)
			if r.Dy() < minLineH || r.Dx() < minSegW {
				continue
			}
			items = append(items, &item{line: i, r: r, ratio: float64(r.Dx()) / float64(r.Dy())})
		}
	}
	if len(items) == 0 {
		return nil, nil
	}
	if len(items) > maxItems {
		// Вся рамка сцены вместо тултипа — много мусора: только самые
		// широкие куски, чтобы не распознавать секунду.
		sort.SliceStable(items, func(i, j int) bool { return items[i].r.Dx() > items[j].r.Dx() })
		items = items[:maxItems]
	}
	en.mu.Lock()
	defer en.mu.Unlock()
	s, err := en.load(model)
	if err != nil {
		return nil, err
	}
	// Пачками по похожей ширине (как RapidOCR): меньше пустого справа.
	order := append([]*item(nil), items...)
	sort.SliceStable(order, func(i, j int) bool { return order[i].ratio < order[j].ratio })
	for at := 0; at < len(order); {
		// В пачку — строки не шире полутора самой узкой в ней: иначе
		// узкие дополняются пустым до широкой и считаются зря.
		end := at + 1
		for end < len(order) && end-at < batch && order[end].ratio <= 1.5*max(order[at].ratio, float64(MinWidth)/Height) {
			end++
		}
		part := order[at:end]
		at = end
		ratio := float64(MinWidth) / Height
		for _, it := range part {
			ratio = max(ratio, it.ratio)
		}
		W := min(int(Height*ratio), MaxWidth)
		data := make([]float32, len(part)*3*Height*W)
		for k, it := range part {
			Prepare(img, it.r, W, data[k*3*Height*W:(k+1)*3*Height*W])
		}
		probs, dims, err := s.run(data, len(part), W)
		if err != nil {
			return nil, err
		}
		if int(dims[0]) != len(part) || int(dims[2]) != len(s.chars) {
			return nil, fmt.Errorf("выход модели %v: словарь %d символов", dims, len(s.chars))
		}
		T, C := int(dims[1]), int(dims[2])
		for k, it := range part {
			it.probs = append([]float32(nil), probs[k*T*C:(k+1)*T*C]...)
			it.T, it.C = T, C
			it.text, it.score = Decode(it.probs, T, C, s.chars)
		}
	}
	var out []Text
	for i, ln := range lines {
		var parts []string
		var segs []Seg
		sum, n := 0.0, 0
		for _, it := range items {
			if it.line != i {
				continue
			}
			t := strings.TrimSpace(it.text)
			if t == "" || it.score < junkScore || (utf8.RuneCountInString(t) <= 2 && it.score < junkShortScore) {
				continue
			}
			parts = append(parts, t)
			segs = append(segs, Seg{Text: t, Probs: it.probs, T: it.T, C: it.C})
			sum += it.score
			n++
		}
		if n == 0 {
			continue
		}
		out = append(out, Text{Text: strings.Join(parts, " "), Score: sum / float64(n), Box: ln.Box.Add(b.Min), Segs: segs})
	}
	return out, nil
}

// Chars — словарь модели (индекс символа → символ; 0 — пусто CTC).
func (en *Engine) Chars(model string) ([]string, error) {
	en.mu.Lock()
	defer en.mu.Unlock()
	s, err := en.load(model)
	if err != nil {
		return nil, err
	}
	return s.chars, nil
}

// Lines — только текст строк.
func Lines(ts []Text) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Text
	}
	return out
}
