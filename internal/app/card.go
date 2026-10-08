package app

import (
	"time"

	"albionzonefix/internal/avalon"
	"albionzonefix/internal/zonecard"
	"albionzonefix/internal/zones"
)

// Карточка зоны (этап 4): последнее нажатие кнопки, отчёт портала на карту
// и риск чёрного экрана по истории переходов.

// expectWindow — сколько после снимка портала считаем, что человек идёт
// именно в эту зону (для вылетов: у них цель неизвестна).
const expectWindow = 10 * time.Minute

// CardView — карточка для вкладки «Зона».
type CardView struct {
	At     time.Time      `json:"at,omitzero"`
	Busy   bool           `json:"busy"`
	Error  string         `json:"error,omitempty"` // zonecard.ErrKind*
	Arg    string         `json:"arg,omitempty"`
	Portal bool           `json:"portal"`
	Zone   *zonecard.Zone `json:"zone,omitempty"`
	Read   string         `json:"read,omitempty"` // как прочитано с экрана
	Doubt  bool           `json:"doubt"`
	// MapHint — подсказать карту мира (M): итог сомнительный, зона не
	// узнана или тултип прочитан обрывками (zonecard.WantsMapHint).
	MapHint bool   `json:"mapHint,omitempty"`
	Alt     string `json:"alt,omitempty"` // второй кандидат при сомнении
	Size    int    `json:"size,omitempty"`
	// ClosesAt — когда портал закроется (снимок + сколько оставалось).
	ClosesAt time.Time `json:"closesAt,omitzero"`
	// Map — итог отправки портала на карту; MapWhy — почему не отправлен
	// (zonecard.Why*, "off" — отправка выключена).
	Map    *avalon.Status `json:"map,omitempty"`
	MapWhy string         `json:"mapWhy,omitempty"`
	// Риск чёрного экрана: Black из Total переходов в эту зону; Risk —
	// показывать (Total ≥ 3 и предупреждения включены).
	Black int  `json:"black"`
	Total int  `json:"total"`
	Risk  bool `json:"risk"`
	// Языки OCR Windows и чего не хватает (ocr.Hint*).
	OCRLangs   []string `json:"ocrLangs,omitempty"`
	OCRHint    string   `json:"ocrHint,omitempty"`
	OCRChecked bool     `json:"ocrChecked"`
}

// CardInfo — состояние снимающего (zonecard.Runner): занят ли, языки OCR.
type CardInfo func() (busy bool, langs []string, hint string, checked bool)

type cardState struct {
	shot  zonecard.Shot
	at    time.Time
	tipTo string // портал ушёл в очередь отправки: куда
	why   string
}

type expectation struct {
	from, to string
	at       time.Time
}

// AttachCard подключает состояние снимающего (для вкладки).
func (a *App) AttachCard(info CardInfo) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cardInfo = info
}

// SetCard — итог нажатия: запоминает карточку, шлёт портал на карту (по
// правилам zonecard.ByButton) и запоминает, куда человек собрался (для
// риска по вылетам). Отдаёт решение о карте для журнала: «портал A → B
// отправляю» или «не отправлено (<код>)» — код zonecard.Why*, "off",
// zonecard.ErrKind* (карточка не вышла), "noPortal" (не тултип портала),
// "noReporter".
func (a *App) SetCard(s zonecard.Shot, d *zonecard.Dict) string {
	set := a.settings.Get()
	a.mu.Lock()
	defer a.mu.Unlock()
	now := s.Result.At
	if now.IsZero() {
		now = time.Now()
	}
	c := &cardState{shot: s, at: now}
	a.card = c
	z := s.Result.Zone()
	switch {
	case s.Kind != "":
		return "не отправлено (" + s.Kind + ")"
	case !s.Result.Portal || z == nil:
		return "не отправлено (noPortal)"
	}
	if a.here != nil && !s.Result.Doubtful() {
		a.expect = &expectation{from: a.here.Zone, to: z.Code, at: now}
	}
	if !set.MapSend {
		c.why = "off"
		return "не отправлено (off)"
	}
	if a.mapRep == nil || d == nil {
		return "не отправлено (noReporter)"
	}
	var here *avalon.Place
	if a.here != nil {
		h := *a.here
		here = &h
	}
	tip, why := zonecard.ByButton(s.Result, here, d)
	if why != "" {
		c.why = why
		return "не отправлено (" + why + ")"
	}
	c.tipTo = tip.To
	a.mapRep.OfferTip(tip) // не блокирует
	return "портал " + tip.From + " → " + tip.To + " отправляю"
}

// wantFor — куда шёл человек при вылете из from (по снимку портала).
// Зовётся под a.mu.
func (a *App) wantFor(tr zones.Transition) string {
	e := a.expect
	if e == nil || tr.OK || tr.To != "" || e.from != tr.From || tr.T.Sub(e.at) > expectWindow || tr.T.Before(e.at) {
		return ""
	}
	return e.to
}

// OnStall — новый сервер не ответил на CONNECT за zonecard.StallAfter
// (предупреждение о чёрном экране): fn(сервер, откуда шли — название).
// Зовётся в своей горутине, только при включённой настройке BlackWarn.
func (a *App) OnStall(fn func(server, fromName string)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tracker.OnStall(zonecard.StallAfter, func(server, from string) {
		if !a.settings.Get().BlackWarn || fn == nil {
			return
		}
		name := ""
		if from != "" {
			name = a.name(from)
		}
		go fn(server, name)
	})
}

// CheckStall — звать часто (раз в 250 мс).
func (a *App) CheckStall(now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tracker.CheckStall(now)
}

// cardView — карточка для State. Зовётся под a.mu; tip — LastTip отправителя.
func (a *App) cardView(tip *avalon.Status, blackWarn bool) *CardView {
	v := &CardView{}
	if a.cardInfo != nil {
		v.Busy, v.OCRLangs, v.OCRHint, v.OCRChecked = a.cardInfo()
	}
	c := a.card
	if c == nil {
		return v
	}
	s := c.shot
	v.At, v.Error, v.Arg = c.at, s.Kind, s.Arg
	v.MapHint = zonecard.WantsMapHint(s)
	if s.Kind != "" {
		return v
	}
	r := s.Result
	v.Portal, v.Zone, v.Read, v.Doubt = r.Portal, r.Zone(), r.Tooltip.Read, r.Doubtful()
	if v.Doubt && len(r.Matches) > 1 {
		v.Alt = r.Matches[1].Zone.Name
	}
	v.Size, v.ClosesAt = r.Tooltip.Size, r.ClosesAt()
	v.MapWhy = c.why
	if c.tipTo != "" && tip != nil && tip.To == c.tipTo && !tip.At.Before(c.at) {
		v.Map = tip
	}
	if v.Zone != nil {
		v.Black, v.Total = zonecard.Risk(a.trs, v.Zone.Code)
		v.Risk = blackWarn && r.Portal && zonecard.ShowRisk(v.Total)
	}
	return v
}
