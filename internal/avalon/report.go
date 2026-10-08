package avalon

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Итог отправки — коды для страницы (она переводит их сама).
const (
	ResSending = "sending" // ушёл, ждём ответа
	ResOK      = "ok"      // 📡 на карте
	ResLimit   = "limit"   // 429 why=limit: лимит отчётов в час
	ResIPLive  = "iplive"  // 429 why=iplive: много непроверенных связей с адреса
	ResRegion  = "region"  // не Европа (своя проверка или 422 why=region)
	ResRefused = "refused" // сервер отказал по другой причине (Why)
	ResOffline = "offline" // сервер карты недоступен
	ResPaused  = "paused"  // сервер был недоступен, ждём до RetryAt — не шлём
)

// Паузы после неудачи: не долбить сервер, который не открывается (в России
// Oracle бывает недоступен), и не упираться в лимит.
const (
	OfflinePause = 5 * time.Minute
	LimitPause   = 15 * time.Minute
	sendTimeout  = 5 * time.Second
)

// Send отправляет один отчёт. Ответ — итог и причина отказа (why).
func Send(ctx context.Context, c *http.Client, base string, r Report) (string, string) {
	body, err := json.Marshal(r)
	if err != nil {
		return ResRefused, err.Error()
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/roads/report", bytes.NewReader(body))
	if err != nil {
		return ResRefused, err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return ResOffline, ""
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var j struct {
		Why string `json:"why"`
	}
	json.Unmarshal(b, &j)
	switch {
	case resp.StatusCode == http.StatusOK:
		return ResOK, ""
	case resp.StatusCode == http.StatusTooManyRequests && j.Why == "iplive":
		return ResIPLive, ""
	case resp.StatusCode == http.StatusTooManyRequests:
		return ResLimit, ""
	case resp.StatusCode == http.StatusUnprocessableEntity && j.Why == "region":
		return ResRegion, ""
	case resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusGatewayTimeout:
		// прокси перед сервером есть, а сам сервер карты не отвечает
		return ResOffline, ""
	}
	if j.Why == "" {
		j.Why = strconv.Itoa(resp.StatusCode)
	}
	return ResRefused, j.Why
}

// Status — последний проход и чем кончилась его отправка.
type Status struct {
	From     string    `json:"from"`
	To       string    `json:"to"`
	FromName string    `json:"fromName"`
	ToName   string    `json:"toName"`
	At       time.Time `json:"at"`
	Result   string    `json:"result"`
	Why      string    `json:"why,omitempty"`
	// RetryAt — до какого времени не шлём (сервер недоступен или лимит).
	RetryAt time.Time `json:"retryAt,omitzero"`
}

// ReporterConfig — что нужно отправителю.
type ReporterConfig struct {
	URL     string               // сервер карты; пусто — ServerURL
	Client  *http.Client         // nil — свой, без переходов по редиректам
	Install func() string        // номер установки
	Now     func() time.Time     // nil — time.Now
	Logf    func(string, ...any) // журнал
}

// Reporter отправляет проходы по одному в своей горутине. Offer не
// блокирует: перехват пакетов ждать сеть не должен.
type Reporter struct {
	cfg ReporterConfig
	q   chan Pass

	mu    sync.Mutex
	pause time.Time
	last  *Status
}

// NewReporter запускает отправителя.
func NewReporter(cfg ReporterConfig) *Reporter {
	r := newReporter(cfg)
	go func() {
		for p := range r.q {
			r.Handle(context.Background(), p)
		}
	}()
	return r
}

func newReporter(cfg ReporterConfig) *Reporter {
	if cfg.URL == "" {
		cfg.URL = ServerURL
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{
			// Отчёт — только на наш адрес: редирект куда-то ещё не идём.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	if cfg.Install == nil {
		cfg.Install = NewInstall
	}
	return &Reporter{cfg: cfg, q: make(chan Pass, 8)}
}

// WhyQueueFull — причина в итоге, когда очередь отправки полна.
const WhyQueueFull = "queue"

// Offer ставит проход в очередь на отправку. Очередь полна — проход
// пропадает (не страшно: следующий будет со следующим порталом), но итог
// не остаётся «отправляется»: вкладка показывает отказ, в журнале строка.
func (r *Reporter) Offer(p Pass) {
	r.set(p, ResSending, "")
	select {
	case r.q <- p:
	default:
		r.cfg.Logf("карта: проход %s → %s пропущен: очередь отправки полна", p.From, p.To)
		r.set(p, ResRefused, WhyQueueFull)
	}
}

// Note запоминает проход, который не отправлен по своей причине (например,
// не Европа): вкладка показывает её.
func (r *Reporter) Note(p Pass, result string) { r.set(p, result, "") }

func (r *Reporter) set(p Pass, result, why string) *Status {
	st := &Status{From: p.From, To: p.To, At: r.cfg.Now(), Result: result, Why: why}
	if z, ok := Lookup(p.From); ok {
		st.FromName = z.Name
	}
	if z, ok := Lookup(p.To); ok {
		st.ToName = z.Name
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if result == ResPaused || result == ResOffline || result == ResLimit {
		st.RetryAt = r.pause
	}
	r.last = st
	return st
}

// Handle отправляет проход сейчас (с учётом паузы) и возвращает итог.
func (r *Reporter) Handle(ctx context.Context, p Pass) Status {
	now := r.cfg.Now()
	r.mu.Lock()
	paused := now.Before(r.pause)
	r.mu.Unlock()
	if paused {
		return *r.set(p, ResPaused, "")
	}
	res, why := Send(ctx, r.cfg.Client, r.cfg.URL, Body(p, r.cfg.Install()))
	r.mu.Lock()
	switch res {
	case ResOffline:
		r.pause = now.Add(OfflinePause)
	case ResLimit:
		r.pause = now.Add(LimitPause)
	}
	r.mu.Unlock()
	if res == ResOK {
		r.cfg.Logf("карта: проход %s → %s принят", p.From, p.To)
	} else {
		r.cfg.Logf("карта: проход %s → %s не принят: %s %s", p.From, p.To, res, why)
	}
	return *r.set(p, res, why)
}

// Last — последний проход и итог; nil — проходов не было.
func (r *Reporter) Last() *Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.last == nil {
		return nil
	}
	c := *r.last
	return &c
}
