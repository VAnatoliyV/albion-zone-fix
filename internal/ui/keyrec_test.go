package ui

import (
	"context"
	"net/http"
	"testing"
	"time"

	"albionzonefix/internal/hotkey"
)

// fakeRec — запись кнопки по команде теста.
type fakeRec struct {
	hint chan string
	res  chan error
	key  chan string
}

func (f *fakeRec) record(ctx context.Context, timeout time.Duration, hint func(string)) (string, error) {
	for {
		select {
		case h := <-f.hint:
			hint(h)
		case k := <-f.key:
			return k, nil
		case err := <-f.res:
			return "", err
		case <-ctx.Done():
			return "", hotkey.ErrCancel
		}
	}
}

func waitRec(t *testing.T, e *env, ok func(KeyRecState) bool) KeyRecState {
	t.Helper()
	var st KeyRecState
	for i := 0; i < 200; i++ {
		st = KeyRecState{}
		e.get(t, "/api/hotkey/record", &st)
		if ok(st) {
			return st
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("не дождались: %+v", st)
	return st
}

func TestKeyRecord(t *testing.T) {
	f := &fakeRec{hint: make(chan string), res: make(chan error), key: make(chan string)}
	e := start(t, func(o *Options) { o.RecordKey = f.record })

	if e.post(t, "/api/hotkey/record", nil, false).StatusCode != http.StatusForbidden {
		t.Fatal("без ключа страницы — нельзя")
	}
	if e.post(t, "/api/hotkey/record", nil, true).StatusCode != 200 {
		t.Fatal("начать запись")
	}
	waitRec(t, e, func(s KeyRecState) bool { return s.Active && s.Seq == 1 })
	e.post(t, "/api/hotkey/record", nil, true) // второй раз — та же запись
	f.hint <- "needMod"
	waitRec(t, e, func(s KeyRecState) bool { return s.Active && s.Hint == "needMod" && s.Seq == 1 })
	f.key <- "Ctrl+Q"
	st := waitRec(t, e, func(s KeyRecState) bool { return !s.Active })
	if st.Result != "ok" || st.Key != "ctrl+q" || st.Seq != 1 {
		t.Fatalf("%+v", st)
	}
	if e.a.Settings().ZoneKey != "ctrl+q" || len(e.hooks) == 0 || e.hooks[len(e.hooks)-1].ZoneKey != "ctrl+q" {
		t.Fatalf("кнопка не сохранена и не применена: %q", e.a.Settings().ZoneKey)
	}

	// Отмена со страницы — кнопка прежняя.
	e.post(t, "/api/hotkey/record", nil, true)
	waitRec(t, e, func(s KeyRecState) bool { return s.Active && s.Seq == 2 })
	if e.post(t, "/api/hotkey/cancel", nil, true).StatusCode != 200 {
		t.Fatal("отмена")
	}
	if st := waitRec(t, e, func(s KeyRecState) bool { return !s.Active }); st.Result != "cancel" || st.Seq != 2 {
		t.Fatalf("%+v", st)
	}
	// Время вышло.
	e.post(t, "/api/hotkey/record", nil, true)
	waitRec(t, e, func(s KeyRecState) bool { return s.Active && s.Seq == 3 })
	f.res <- hotkey.ErrTimeout
	if st := waitRec(t, e, func(s KeyRecState) bool { return !s.Active }); st.Result != "timeout" {
		t.Fatalf("%+v", st)
	}
	if e.a.Settings().ZoneKey != "ctrl+q" {
		t.Fatal("отмена и таймаут кнопку не меняют")
	}
}

func TestKeyRecordUnavailable(t *testing.T) {
	e := start(t)
	if e.post(t, "/api/hotkey/record", nil, true).StatusCode != 400 {
		t.Fatal("без записи — ошибка")
	}
}
