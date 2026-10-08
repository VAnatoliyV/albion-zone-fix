package zonecard

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Коды беды карточки для страницы (zn.err.*).
const (
	ErrKindCapture   = "capture"   // снимок не вышел (Arg — причина)
	ErrKindOCR       = "ocr"       // распознавание не вышло (Arg — причина)
	ErrKindNoLang    = "noLang"    // нет ни русского, ни английского OCR
	ErrKindNoTooltip = "noTooltip" // под курсором не портал дорог
	ErrKindUnknown   = "unknown"   // тултип есть, зона не узнана (Arg — что прочитано)
	ErrKindNoDict    = "noDict"    // справочник зон не прочитался
	ErrKindBlank     = "blank"     // снимок пустой: чёрный или застывший (Arg — EmptyBlack/EmptySame)
)

// Пустой снимок (как screen.EmptyBlack/EmptySame).
const (
	EmptyBlack = "black"
	EmptySame  = "same"
)

// Snap — что сказал снимающий: строка для журнала и пустой ли снимок.
type Snap struct {
	Info  string
	Empty string // EmptyBlack, EmptySame или ""
}

// Повторы (Retry в RunnerConfig): тултип мог ещё не дорисоваться или
// мигнуть, текст мог прочитаться плохо — ещё снимки и варианты картинки,
// пока не выйдет уверенно или не кончится время.
const (
	RetryBudget = 600 * time.Millisecond // новый шаг после этого не начинаем
	RetryGap    = 120 * time.Millisecond // между снимками
	RetryShots  = 3                      // снимков на нажатие, не больше
)

// Варианты картинки для OCR (как screen.VarGray/VarInvert); "" — цветная.
const (
	VarColor  = ""
	VarGray   = "gray"
	VarInvert = "inv"
)

// Shot — итог одного нажатия.
type Shot struct {
	Result  Result
	Kind    string // "" — удачно; иначе ErrKind*
	Arg     string
	Took    time.Duration
	Capture string // что снято (для журнала)
	// Замер для журнала: снимок, распознавание и на каких языках (по
	// порядку вызовов: «en-US», «en-US, ru» — второй язык понадобился).
	CaptureTook, OCRTook time.Duration
	OCRLangs             string
	// Image — картинка, по которой вышел итог (при повторах — не обязательно
	// Path), Source — цветной снимок, из которого она сделана. Try — какой
	// снимок и вариант дал итог («2/3 серый»), Tries — все попытки по
	// порядку для журнала.
	Image  string
	Source string
	Try    string
	Tries  []string
}

// RunnerConfig — что нужно снимающему (на Windows — снимок GDI и OCR через
// PowerShell, в тестах — подделки).
type RunnerConfig struct {
	Path      string // куда класть снимок (PNG в каталоге данных)
	Capture   func(path string) (Snap, error)
	Recognize func(ctx context.Context, path string, langs []string) (map[string][]string, error)
	Languages func(ctx context.Context) ([]string, error)
	Pick      func(installed []string) []string // какие языки пробовать
	Hint      func(installed []string) string   // чего не хватает
	Dict      *Dict
	Now       func() time.Time
	Done      func(Shot)
	Logf      func(string, ...any)
	// Live — рабочий PowerShell жив (OCR дёшев). false — каждый вызов OCR
	// стал бы отдельным разовым PowerShell (~1 с), поэтому все языки одним
	// вызовом, как в 1.0.3. nil — считать живым.
	Live func() bool
	// Gap — сколько не принимать новое нажатие после конца прошлого
	// (дребезг кнопки и автоповтор F-клавиши).
	Gap time.Duration
	// Retry — при неуверенном итоге снимать ещё (до RetryShots снимков за
	// RetryBudget). false — один снимок, как раньше.
	Retry bool
	// Prepare делает вариант kind (VarGray, VarInvert) картинки src в dst;
	// nil — без вариантов. Variants — включены ли варианты сейчас (nil — да).
	Prepare  func(src, dst, kind string) error
	Variants func() bool
	// Sleep — пауза между снимками (в тестах — без ожидания).
	Sleep func(time.Duration)
}

// Runner — снимок, распознавание и опознание по нажатию, по одному за раз.
type Runner struct {
	cfg RunnerConfig

	mu       sync.Mutex
	busy     bool
	lastEnd  time.Time
	langs    []string // установленные языки OCR
	checked  bool     // языки проверены
	hint     string   // чего не хватает (ocr.Hint*), "" — всё есть
	checkErr string
	lastLang string // язык OCR, на котором в прошлый раз нашёлся тултип
}

// NewRunner готовит снимающего.
func NewRunner(cfg RunnerConfig) *Runner {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	if cfg.Done == nil {
		cfg.Done = func(Shot) {}
	}
	if cfg.Sleep == nil {
		cfg.Sleep = time.Sleep
	}
	return &Runner{cfg: cfg}
}

// CheckLanguages — какие языки OCR установлены (при запуске; долго — звать
// в горутине).
func (r *Runner) CheckLanguages(ctx context.Context) {
	if r.cfg.Languages == nil {
		return
	}
	langs, err := r.cfg.Languages(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checked = true
	if err != nil {
		r.checkErr = err.Error()
		r.hint = "check"
		r.cfg.Logf("карточка зоны: языки OCR не проверены: %v", err)
		return
	}
	r.langs, r.checkErr = langs, ""
	if r.cfg.Hint != nil {
		r.hint = r.cfg.Hint(langs)
	}
	r.cfg.Logf("карточка зоны: языки OCR %v, подсказка %q", langs, r.hint)
}

// OCRStatus — установленные языки OCR и чего не хватает; checked=false —
// ещё не проверяли.
func (r *Runner) OCRStatus() (langs []string, hint string, checked bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.langs...), r.hint, r.checked
}

// Busy — снимает прямо сейчас.
func (r *Runner) Busy() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.busy
}

// Trigger — нажали кнопку. Занят или нажатие слишком рано после прошлого —
// false. Работа — в своей горутине, итог — в Done.
func (r *Runner) Trigger() bool {
	r.mu.Lock()
	now := r.cfg.Now()
	if r.busy || (!r.lastEnd.IsZero() && now.Sub(r.lastEnd) < r.cfg.Gap) {
		r.mu.Unlock()
		return false
	}
	r.busy = true
	r.mu.Unlock()
	go func() {
		s := r.Run(context.Background())
		r.mu.Lock()
		r.busy, r.lastEnd = false, r.cfg.Now()
		r.mu.Unlock()
		r.cfg.Done(s)
	}()
	return true
}

// Run — одно снятие сейчас (без проверки «занят»).
func (r *Runner) Run(ctx context.Context) Shot {
	start := r.cfg.Now()
	s := r.run(ctx, start)
	s.Took = r.cfg.Now().Sub(start)
	took := fmt.Sprintf("за %v (снимок %v, OCR %v: %s)", s.Took.Round(time.Millisecond),
		s.CaptureTook.Round(time.Millisecond), s.OCRTook.Round(time.Millisecond), s.OCRLangs)
	if len(s.Tries) > 1 {
		took += fmt.Sprintf("; попытки: %s; итог — %s", strings.Join(s.Tries, ", "), s.Try)
	}
	if s.Kind == "" {
		z := s.Result.Zone()
		r.cfg.Logf("карточка зоны: %q → %s (%s, %.2f, %s) %s; %s", s.Result.Tooltip.Read, z.Name, z.Code,
			s.Result.Matches[0].Closeness, s.Result.Lang, took, s.Capture)
	} else {
		r.cfg.Logf("карточка зоны: %s %s %s; %s", s.Kind, s.Arg, took, s.Capture)
	}
	return s
}

func (r *Runner) run(ctx context.Context, at time.Time) Shot {
	if r.cfg.Dict == nil {
		return Shot{Kind: ErrKindNoDict}
	}
	r.mu.Lock()
	langs, hint, checked, last := r.langs, r.hint, r.checked, r.lastLang
	r.mu.Unlock()
	if checked && hint == "none" {
		return Shot{Kind: ErrKindNoLang}
	}
	try := langs
	if r.cfg.Pick != nil {
		try = r.cfg.Pick(langs)
	}
	// Сначала язык, на котором тултип нашёлся в прошлый раз; второй — только
	// если на первом нет уверенно опознанного тултипа (тогда язык выберет
	// Choose по оценке).
	live := r.cfg.Live == nil || r.cfg.Live()
	if !live {
		last = "" // рабочего нет: один разовый PowerShell на все языки
	}
	first, rest := LangPlan(try, last)

	// Повторы и варианты — только с живым рабочим PowerShell: разовый
	// стоит ~1 с на вызов.
	shots := 1
	if r.cfg.Retry && live {
		shots = RetryShots
	}
	variants := []string{VarColor}
	if shots > 1 && r.cfg.Prepare != nil && (r.cfg.Variants == nil || r.cfg.Variants()) {
		variants = append(variants, VarGray, VarInvert)
	}

	t0 := time.Now()
	over := func() bool { return time.Since(t0) >= RetryBudget }
	var (
		best             Shot
		have             bool
		capTook, ocrTook time.Duration
		usedLangs        string
		tries            []string
		empty            string
		lastCap          time.Time
	)
	keep := func(s Shot, try string) bool {
		s.Try = try
		tries = append(tries, try+" "+shotWord(s))
		if !have || Quality(s) > Quality(best) {
			best, have = s, true
		}
		return Good(s)
	}
loop:
	for n := 1; n <= shots; n++ {
		if n > 1 {
			if over() {
				break
			}
			if d := RetryGap - time.Since(lastCap); d > 0 {
				r.cfg.Sleep(d)
			}
		}
		path := shotPath(r.cfg.Path, n, shots)
		lastCap = time.Now()
		snap, err := r.cfg.Capture(path)
		capTook += time.Since(lastCap)
		if err != nil {
			if keep(Shot{Kind: ErrKindCapture, Arg: err.Error(), Capture: snap.Info}, fmt.Sprintf("%d/%d", n, shots)) {
				break
			}
			continue
		}
		if snap.Empty == EmptySame && n > 1 {
			// Повтор совпал с прошлым снимком этого же нажатия — статичная
			// сцена (BitBlt курсор не снимает), а не застывшая игра.
			snap.Empty = ""
		}
		if snap.Empty != "" {
			empty = snap.Empty
			r.cfg.Logf("карточка зоны: снимок пустой (%s; полноэкранный режим?) %s", snap.Empty, snap.Info)
			if snap.Empty == EmptyBlack {
				// Чёрный — распознавать нечего.
				if keep(Shot{Kind: ErrKindBlank, Arg: EmptyBlack, Capture: snap.Info, Image: path, Source: path}, fmt.Sprintf("%d/%d", n, shots)) {
					break
				}
				continue
			}
		}
		colorPartly := false
		for _, v := range variants {
			if v != VarColor && over() {
				break loop
			}
			if v != VarColor && !colorPartly {
				// Тултипа на цветном нет — скорее не дорисовался или мигнул:
				// нужнее новый снимок, чем варианты этой картинки.
				break
			}
			img := path
			if v != VarColor {
				img = variantPath(path, v)
				if err := r.cfg.Prepare(path, img, v); err != nil {
					r.cfg.Logf("карточка зоны: вариант %s не вышел: %v", v, err)
					continue
				}
			}
			t1 := time.Now()
			s, used := r.recognize(ctx, img, first, rest, at)
			ocrTook += time.Since(t1)
			if usedLangs == "" {
				usedLangs = used
			}
			s.Capture, s.Image, s.Source = snap.Info, img, path
			if v == VarColor {
				colorPartly = partly(s)
			}
			if keep(s, fmt.Sprintf("%d/%d %s", n, shots, varWord(v))) {
				break loop
			}
		}
	}
	best.CaptureTook, best.OCRTook, best.OCRLangs = capTook, ocrTook, usedLangs
	if shots > 1 {
		best.Tries = tries
	}
	// Ничего не прочитано, а снимок пустой — подсказка про режим игры.
	if empty != "" && (best.Kind == ErrKindNoTooltip || best.Kind == ErrKindOCR || best.Kind == ErrKindBlank) {
		best.Kind, best.Arg = ErrKindBlank, empty
	}
	if best.Kind == "" && best.Result.Portal {
		r.mu.Lock()
		r.lastLang = best.Result.Lang
		r.mu.Unlock()
	}
	return best
}

// recognize — OCR картинки img на языках first (и rest, если на first нет
// уверенного тултипа) и опознание. used — какие языки понадобились.
func (r *Runner) recognize(ctx context.Context, img string, first, rest []string, at time.Time) (Shot, string) {
	used := strings.Join(first, ", ")
	byLang, err := r.cfg.Recognize(ctx, img, first)
	if len(rest) > 0 && !Sure(r.cfg.Dict, byLang, first, at) {
		more, err2 := r.cfg.Recognize(ctx, img, rest)
		used += "; " + strings.Join(rest, ", ")
		if byLang == nil {
			byLang = map[string][]string{}
		}
		for k, v := range more {
			byLang[k] = v
		}
		if err2 != nil {
			err = err2
		}
	}
	var s Shot
	if err != nil && len(byLang) == 0 {
		s.Kind, s.Arg = ErrKindOCR, err.Error()
		return s, used
	}
	res, err := Choose(r.cfg.Dict, byLang, append(append([]string(nil), first...), rest...), at)
	if err != nil {
		var u UnknownError
		if errors.As(err, &u) {
			s.Kind, s.Arg = ErrKindUnknown, u.Read
			return s, used
		}
		s.Kind = ErrKindNoTooltip
		return s, used
	}
	s.Result = res
	return s, used
}

// Good — итог, лучше которого повторами не добыть: тултип портала с
// временем, название опознано без сомнений.
func Good(s Shot) bool {
	return s.Kind == "" && s.Result.Portal && s.Result.Tooltip.Left > 0 && !s.Result.Doubtful()
}

// Quality — оценка итога для выбора среди повторов: удачное опознание
// лучше любой ошибки; среди удачных — портал, время, уверенность, сходство.
func Quality(s Shot) float64 {
	switch s.Kind {
	case "":
		q := 100.0
		if s.Result.Portal {
			q += 40
		}
		if s.Result.Tooltip.Left > 0 {
			q += 20
		}
		if !s.Result.Doubtful() {
			q += 30 // правильное имя важнее времени
		}
		if len(s.Result.Matches) > 0 {
			q += 10 * s.Result.Matches[0].Closeness
		}
		return q
	case ErrKindUnknown:
		return 50
	case ErrKindNoTooltip:
		return 20
	case ErrKindBlank:
		return 15
	case ErrKindOCR:
		return 10
	}
	return 0
}

// partly — на цветной картинке тултип найден хотя бы отчасти: имя без
// признака портала, без времени, сомнительно или зона не узнана. Тогда
// варианты картинки могут дочитать текст; без тултипа нужнее новый снимок.
func partly(s Shot) bool {
	switch shotWord(s) {
	case "noPortal", "noTime", "doubt", ErrKindUnknown:
		return true
	}
	return false
}

// shotWord — итог попытки одним словом для журнала (как в «карта: …»).
func shotWord(s Shot) string {
	switch {
	case s.Kind != "":
		return s.Kind
	case !s.Result.Portal:
		return "noPortal"
	case s.Result.Tooltip.Left <= 0:
		return "noTime"
	case s.Result.Doubtful():
		return "doubt"
	}
	return "ok"
}

func varWord(v string) string {
	switch v {
	case VarGray:
		return "серый"
	case VarInvert:
		return "инверсия"
	}
	return "цвет"
}

// shotPath — куда класть n-й снимок: первый — в path, как раньше; при
// повторах — рядом с номером (zone-capture-2.png).
func shotPath(path string, n, of int) string {
	if n <= 1 || of <= 1 {
		return path
	}
	return withSuffix(path, fmt.Sprintf("-%d", n))
}

func variantPath(path, v string) string { return withSuffix(path, "-"+v) }

func withSuffix(path, suf string) string {
	ext := filepath.Ext(path)
	return strings.TrimSuffix(path, ext) + suf + ext
}

// SureCloseness — тултип опознан так уверенно, что второй язык OCR не нужен.
// Английский клиент на русском движке читается хуже («Mawor Согде» → 0.64),
// на своём — 1.00.
const SureCloseness = 0.9

// LangPlan — порядок распознавания: last (язык прошлого удачного тултипа)
// отдельно первым, остальные — потом и только при надобности. last неизвестен
// или не среди try — все языки сразу одним вызовом (как раньше).
func LangPlan(try []string, last string) (first, rest []string) {
	idx := -1
	for i, l := range try {
		if l == last {
			idx = i
			break
		}
	}
	if last == "" || idx < 0 || len(try) < 2 {
		return try, nil
	}
	first = []string{try[idx]}
	for i, l := range try {
		if i != idx {
			rest = append(rest, l)
		}
	}
	return first, rest
}

// Sure — на языках langs уже есть тултип портала, опознанный не хуже
// SureCloseness и не сомнительный (Result.Doubtful: и по отрыву от второго
// кандидата): остальные языки можно не распознавать.
func Sure(d *Dict, byLang map[string][]string, langs []string, at time.Time) bool {
	r, err := Choose(d, byLang, langs, at)
	return err == nil && r.Portal && len(r.Matches) > 0 && r.Matches[0].Closeness >= SureCloseness && !r.Doubtful()
}
