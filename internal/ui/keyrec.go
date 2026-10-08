package ui

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"albionzonefix/internal/hotkey"
)

// RecordTimeout — сколько ждать нажатия при записи кнопки карточки.
const RecordTimeout = 10 * time.Second

// KeyRecState — запись кнопки карточки для страницы («Нажми кнопку…»).
type KeyRecState struct {
	Active bool   `json:"active"`
	Hint   string `json:"hint,omitempty"`   // needMod — нажали голую клавишу
	Result string `json:"result,omitempty"` // ok, cancel, timeout, error, unsupported
	Key    string `json:"key,omitempty"`    // записанная кнопка (result=ok)
	Error  string `json:"error,omitempty"`
	Seq    int    `json:"seq"` // номер записи: страница отличает старый итог от нового
}

type keyRec struct {
	mu     sync.Mutex
	st     KeyRecState
	cancel context.CancelFunc
}

func (s *Server) keyRecState() KeyRecState {
	s.rec.mu.Lock()
	defer s.rec.mu.Unlock()
	return s.rec.st
}

// startKeyRec — «Нажми кнопку…»: ждём нажатие хуками программы (не
// событиями страницы: WebView2 съедает боковые кнопки как «назад/вперёд», а
// записать кнопку можно и не из окна). Записанная кнопка сразу сохраняется
// в настройках и через OnSettings становится рабочей.
func (s *Server) startKeyRec(w http.ResponseWriter, r *http.Request) {
	if s.o.RecordKey == nil {
		reply(w, errors.New("запись кнопки недоступна"))
		return
	}
	s.rec.mu.Lock()
	if s.rec.st.Active {
		st := s.rec.st
		s.rec.mu.Unlock()
		s.json(w, st)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.rec.cancel = cancel
	s.rec.st = KeyRecState{Active: true, Seq: s.rec.st.Seq + 1}
	seq, st := s.rec.st.Seq, s.rec.st
	s.rec.mu.Unlock()

	go func() {
		defer cancel()
		k, err := s.o.RecordKey(ctx, RecordTimeout, func(h string) {
			s.rec.mu.Lock()
			if s.rec.st.Seq == seq {
				s.rec.st.Hint = h
			}
			s.rec.mu.Unlock()
		})
		res := KeyRecState{Seq: seq}
		switch {
		case err == nil:
			res.Result, res.Key = "ok", string(hotkey.Normalize(k))
			if serr := s.saveZoneKey(res.Key); serr != nil {
				res.Result, res.Error = "error", serr.Error()
			}
		case errors.Is(err, hotkey.ErrCancel):
			res.Result = "cancel"
		case errors.Is(err, hotkey.ErrTimeout):
			res.Result = "timeout"
		case errors.Is(err, hotkey.ErrUnsupported):
			res.Result = "unsupported"
		default:
			res.Result, res.Error = "error", err.Error()
		}
		s.rec.mu.Lock()
		s.rec.st, s.rec.cancel = res, nil
		s.rec.mu.Unlock()
	}()
	s.json(w, st)
}

func (s *Server) cancelKeyRec(w http.ResponseWriter, r *http.Request) {
	s.rec.mu.Lock()
	if s.rec.cancel != nil {
		s.rec.cancel()
	}
	s.rec.mu.Unlock()
	reply(w, nil)
}

func (s *Server) saveZoneKey(k string) error {
	old := s.a.Settings()
	cur := old
	cur.ZoneKey = k
	if err := s.a.SetSettings(cur); err != nil {
		return err
	}
	if s.o.OnSettings != nil {
		return s.o.OnSettings(old, s.a.Settings())
	}
	return nil
}
