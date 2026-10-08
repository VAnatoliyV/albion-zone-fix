package zonecard

import (
	"context"
	"errors"
	"fmt"
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
}

// RunnerConfig — что нужно снимающему (на Windows — снимок GDI и OCR через
// PowerShell, в тестах — подделки).
type RunnerConfig struct {
	Path      string // куда класть снимок (PNG в каталоге данных)
	Capture   func(path string) (string, error)
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
	langs, hint, checked := r.langs, r.hint, r.checked
	r.mu.Unlock()
	if checked && hint == "none" {
		return Shot{Kind: ErrKindNoLang}
	}
	try := langs
	if r.cfg.Pick != nil {
		try = r.cfg.Pick(langs)
	}
	t0 := time.Now()
	info, err := r.cfg.Capture(r.cfg.Path)
	capTook := time.Since(t0)
	if err != nil {
		return Shot{Kind: ErrKindCapture, Arg: err.Error(), Capture: info, CaptureTook: capTook}
	}
	r.mu.Lock()
	last := r.lastLang
	r.mu.Unlock()
	// Сначала язык, на котором тултип нашёлся в прошлый раз; второй — только
	// если на первом нет уверенно опознанного тултипа (тогда язык выберет
	// Choose по оценке).
	if r.cfg.Live != nil && !r.cfg.Live() {
		last = "" // рабочего нет: один разовый PowerShell на все языки
	}
	first, rest := LangPlan(try, last)
	t1 := time.Now()
	used := strings.Join(first, ", ")
	byLang, err := r.cfg.Recognize(ctx, r.cfg.Path, first)
	if len(rest) > 0 && !Sure(r.cfg.Dict, byLang, first, at) {
		more, err2 := r.cfg.Recognize(ctx, r.cfg.Path, rest)
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
	s := Shot{Capture: info, CaptureTook: capTook, OCRTook: time.Since(t1), OCRLangs: used}
	if err != nil && len(byLang) == 0 {
		s.Kind, s.Arg = ErrKindOCR, err.Error()
		return s
	}
	res, err := Choose(r.cfg.Dict, byLang, append(append([]string(nil), first...), rest...), at)
	if err != nil {
		var u UnknownError
		if errors.As(err, &u) {
			s.Kind, s.Arg = ErrKindUnknown, u.Read
			return s
		}
		s.Kind = ErrKindNoTooltip
		return s
	}
	if res.Portal {
		r.mu.Lock()
		r.lastLang = res.Lang
		r.mu.Unlock()
	}
	s.Result = res
	return s
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
