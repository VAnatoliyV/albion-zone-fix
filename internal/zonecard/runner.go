package zonecard

import (
	"context"
	"errors"
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
	if s.Kind == "" {
		z := s.Result.Zone()
		r.cfg.Logf("карточка зоны: %q → %s (%s, %.2f, %s) за %v; %s", s.Result.Tooltip.Read, z.Name, z.Code,
			s.Result.Matches[0].Closeness, s.Result.Lang, s.Took.Round(time.Millisecond), s.Capture)
	} else {
		r.cfg.Logf("карточка зоны: %s %s за %v; %s", s.Kind, s.Arg, s.Took.Round(time.Millisecond), s.Capture)
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
	info, err := r.cfg.Capture(r.cfg.Path)
	if err != nil {
		return Shot{Kind: ErrKindCapture, Arg: err.Error(), Capture: info}
	}
	byLang, err := r.cfg.Recognize(ctx, r.cfg.Path, try)
	if err != nil && len(byLang) == 0 {
		return Shot{Kind: ErrKindOCR, Arg: err.Error(), Capture: info}
	}
	res, err := Choose(r.cfg.Dict, byLang, try, at)
	if err != nil {
		var u UnknownError
		if errors.As(err, &u) {
			return Shot{Kind: ErrKindUnknown, Arg: u.Read, Capture: info}
		}
		return Shot{Kind: ErrKindNoTooltip, Capture: info}
	}
	return Shot{Result: res, Capture: info}
}
