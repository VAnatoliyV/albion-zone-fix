package pwsh

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// parsed — вывод в духе ocr.Parse (сам ocr здесь не импортировать: на
// Windows ocr зависит от pwsh).
type parsed struct {
	Lines   map[string][]string
	Langs   []string
	Skipped []string
}

func parse(out []byte) parsed {
	p := parsed{Lines: map[string][]string{}}
	for _, l := range strings.Split(string(out), "\n") {
		var j struct {
			Kind, Tag, Lang string
			Lines           json.RawMessage
		}
		if json.Unmarshal([]byte(l), &j) != nil {
			continue
		}
		switch j.Kind {
		case "lang":
			p.Langs = append(p.Langs, j.Tag)
		case "skip":
			p.Skipped = append(p.Skipped, j.Lang)
		case "ocr":
			var arr []string
			if json.Unmarshal(j.Lines, &arr) != nil {
				var one string
				json.Unmarshal(j.Lines, &one)
				arr = []string{one}
			}
			p.Lines[j.Lang] = arr
		}
	}
	return p
}

func TestStdin(t *testing.T) {
	if in := Stdin("a\r\nb"); in != "a\nb\n\n" {
		t.Fatalf("%q", in)
	}
}

func TestWorkerScriptShape(t *testing.T) {
	for i, r := range WorkerScript {
		if r > 127 {
			t.Fatalf("не ASCII в скрипте на %d: %q", i, string(r))
		}
	}
	// Пустая строка внутри завершила бы ввод многострочной конструкции.
	for i, l := range strings.Split(strings.TrimRight(WorkerScript, "\n"), "\n") {
		if strings.TrimSpace(l) == "" {
			t.Fatalf("пустая строка %d", i+1)
		}
	}
	for _, s := range []string{"function AjReq([int]$id, [string]$b64)", "IAsyncOperation`1", "'@@AJ@@ '",
		"AjSend 0 @{ready=$true}", "TryCreateFromLanguage", "AjEngines[$tag] = $eng", "CreateToastNotifier",
		"AvailableRecognizerLanguages", "UTF8Encoding $false", "$stream.Dispose()"} {
		if !strings.Contains(WorkerScript, s) {
			t.Errorf("в скрипте нет %q", s)
		}
	}
	// Скобки сбалансированы (грубая проверка синтаксиса без PowerShell).
	for _, p := range [][2]string{{"{", "}"}, {"(", ")"}, {"[", "]"}} {
		if a, b := strings.Count(WorkerScript, p[0]), strings.Count(WorkerScript, p[1]); a != b {
			t.Errorf("%s %d, %s %d", p[0], a, p[1], b)
		}
	}
}

func TestProtocol(t *testing.T) {
	line, err := encodeRequest(7, Request{Cmd: "ocr", Path: `C:\Users\Анатолий\zone-capture.png`, Langs: []string{"en-US", "ru"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range line {
		if r > 127 {
			t.Fatalf("запрос не ASCII: %q", line)
		}
	}
	if !strings.HasPrefix(line, "AjReq 7 '") || !strings.HasSuffix(line, "'\n\n") {
		t.Fatalf("%q", line)
	}
	b, _ := base64.StdEncoding.DecodeString(strings.TrimSuffix(strings.TrimPrefix(line, "AjReq 7 '"), "'\n\n"))
	var back Request
	if json.Unmarshal(b, &back) != nil || back.Path != `C:\Users\Анатолий\zone-capture.png` || len(back.Langs) != 2 {
		t.Fatalf("%s", b)
	}

	// Маркер может стоять после приглашения PowerShell.
	enc := base64.StdEncoding.EncodeToString([]byte(`{"items":[]}`))
	if id, body, ok, err := parseReply("PS C:\\> @@AJ@@ 12 " + enc + "\r"); !ok || err != nil || id != 12 || string(body) != `{"items":[]}` {
		t.Fatal(id, string(body), ok, err)
	}
	if _, _, ok, _ := parseReply("WARNING: что-то"); ok {
		t.Fatal("посторонняя строка")
	}
	for _, bad := range []string{"@@AJ@@ 1", "@@AJ@@ x " + enc, "@@AJ@@ 1 !!!", "@@AJ@@ 1 " + base64.StdEncoding.EncodeToString([]byte("{oops"))} {
		if _, _, ok, err := parseReply(bad); !ok || err == nil {
			t.Errorf("%q: ok=%v err=%v", bad, ok, err)
		}
	}

	// Элементы → вывод разового скрипта: его разбирает ocr.Parse.
	body := `{"items":[{"kind":"ocr","lang":"ru","lines":["Путь Авалона в","Cebos-Avemlum"]},{"kind":"ocr","lang":"en-US","lines":"Road of Avalon to"},{"kind":"skip","lang":"es-ES"}]}`
	out, err := itemsOut([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	o := parse(out)
	if len(o.Lines["ru"]) != 2 || o.Lines["en-US"][0] != "Road of Avalon to" || o.Skipped[0] != "es-ES" {
		t.Fatalf("%+v", o)
	}
	// PowerShell 5.1: массив в {"value":…}, один элемент без массива, null.
	for in, n := range map[string]int{
		`{"items":{"value":[{"kind":"lang","tag":"en-US"},{"kind":"lang","tag":"ru"}],"Count":2}}`: 2,
		`{"items":{"kind":"lang","tag":"en-US"}}`:                                                  1,
		`{"items":null}`: 0,
		`{"items":[]}`:   0,
	} {
		out, err := itemsOut([]byte(in))
		if o := parse(out); err != nil || len(o.Langs) != n {
			t.Errorf("%s: %v %v", in, o.Langs, err)
		}
	}
	if _, err := itemsOut([]byte(`{"items":"x"}`)); err == nil {
		t.Error("строка вместо элементов")
	}
	if m := ErrorMsg([]byte("{\"kind\":\"error\",\"msg\":\"Element not found\"}\n")); m != "Element not found" {
		t.Error(m)
	}
	if ErrorMsg(out) != "" {
		t.Error("ошибки нет")
	}
	if readyReply([]byte(`{"ready":true}`)) != nil || readyReply([]byte(`{"error":"WinRT"}`)).Error() != "WinRT" || readyReply([]byte(`{}`)) == nil {
		t.Error("готовность")
	}
}

// fakeProc — подставной PowerShell: читает строки AjReq, отвечает handle.
type fakeProc struct {
	inR  *io.PipeReader
	inW  *io.PipeWriter
	outR *io.PipeReader
	outW *io.PipeWriter
	once sync.Once
	dead chan struct{}
}

func (p *fakeProc) Write(b []byte) (int, error) { return p.inW.Write(b) }
func (p *fakeProc) Read(b []byte) (int, error)  { return p.outR.Read(b) }
func (p *fakeProc) Tail() string                { return "fake stderr" }
func (p *fakeProc) Kill() {
	p.once.Do(func() {
		close(p.dead)
		p.inR.CloseWithError(io.EOF)
		p.outW.Close()
	})
}

func reply(id int, body string) string {
	return fmt.Sprintf("@@AJ@@ %d %s\n", id, base64.StdEncoding.EncodeToString([]byte(body)))
}

// fakeShell: ready — что ответить при запуске ("" — молчать); handle —
// ответ на запрос (строки целиком; "" — молчать, "EXIT" — умереть).
type fakeShell struct {
	starts atomic.Int32
	ready  func(n int) string
	handle func(id int, r Request) string
}

func (f *fakeShell) start() (Proc, error) {
	n := int(f.starts.Add(1))
	p := &fakeProc{dead: make(chan struct{})}
	p.inR, p.inW = io.Pipe()
	p.outR, p.outW = io.Pipe()
	go func() {
		if r := f.ready(n); r != "" {
			if r == "EXIT" {
				p.Kill()
				return
			}
			io.WriteString(p.outW, r)
		}
		sc := bufio.NewScanner(p.inR)
		sc.Buffer(nil, 1<<20)
		for sc.Scan() {
			l := sc.Text()
			if !strings.HasPrefix(l, "AjReq ") {
				continue
			}
			f2 := strings.Fields(l)
			id, _ := strconv.Atoi(f2[1])
			b, _ := base64.StdEncoding.DecodeString(strings.Trim(f2[2], "'"))
			var r Request
			json.Unmarshal(b, &r)
			switch out := f.handle(id, r); out {
			case "":
			case "EXIT":
				p.Kill()
				return
			default:
				io.WriteString(p.outW, out)
			}
		}
	}()
	return p, nil
}

func okReady(int) string { return "PS> warming\n" + reply(0, `{"ready":true}`) }

func testWorker(f *fakeShell) (*Worker, *[]string, *time.Time) {
	var mu sync.Mutex
	var logs []string
	now := time.Unix(1000, 0)
	w := NewWorker(f.start, func(format string, a ...any) {
		mu.Lock()
		logs = append(logs, fmt.Sprintf(format, a...))
		mu.Unlock()
	})
	w.now = func() time.Time { return now }
	w.readyTimeout, w.reqTimeout, w.waitStart = 300*time.Millisecond, 200*time.Millisecond, 500*time.Millisecond
	return w, &logs, &now
}

func TestWorkerOK(t *testing.T) {
	f := &fakeShell{ready: okReady, handle: func(id int, r Request) string {
		if r.Cmd == "toast" {
			return reply(id, `{"items":[{"kind":"error","msg":"AUMID"}]}`)
		}
		return "garbage line\n" + reply(id, `{"items":[{"kind":"ocr","lang":"`+r.Langs[0]+`","lines":["Road of Avalon to"]}]}`)
	}}
	w, _, _ := testWorker(f)
	w.Warm()
	for i := 0; i < 3; i++ {
		out, err := w.Do(context.Background(), Request{Cmd: "ocr", Path: "x.png", Langs: []string{"en-US"}})
		if err != nil || len(parse(out).Lines["en-US"]) != 1 {
			t.Fatalf("%d: %s %v", i, out, err)
		}
	}
	if f.starts.Load() != 1 {
		t.Fatalf("запусков %d", f.starts.Load())
	}
	// Ошибка внутри скрипта, и разовый ошибается так же — ошибка общая,
	// рабочий не виноват.
	oneShot := 0
	_, how, err := w.Call(context.Background(), Request{Cmd: "toast"}, func() ([]byte, error) { oneShot++; return nil, errors.New("exit status 1") })
	if how != "рабочий" || err == nil || err.Error() != "AUMID" || oneShot != 1 || f.starts.Load() != 1 || !w.Live() {
		t.Fatal(how, err, oneShot)
	}
	w.Stop()
	if _, err := w.Do(context.Background(), Request{Cmd: "langs"}); !errors.Is(err, ErrOff) {
		t.Fatal(err)
	}
}

func TestWorkerFailuresAndRestart(t *testing.T) {
	var mode atomic.Value
	mode.Store("ok")
	f := &fakeShell{ready: okReady, handle: func(id int, r Request) string {
		switch mode.Load().(string) {
		case "corrupt":
			return "@@AJ@@ " + strconv.Itoa(id) + " not-base64!!\n"
		case "wrongid":
			return reply(id+5, `{"items":[]}`)
		case "hang":
			return ""
		case "die":
			return "EXIT"
		}
		return reply(id, `{"items":[]}`)
	}}
	w, logs, now := testWorker(f)
	call := func() (string, error) {
		_, how, err := w.Call(context.Background(), Request{Cmd: "langs"}, func() ([]byte, error) { return []byte("{\"kind\":\"lang\",\"tag\":\"ru\"}\n"), nil })
		return how, err
	}
	if how, err := call(); how != "рабочий" || err != nil {
		t.Fatal(how, err)
	}
	for i, m := range []string{"corrupt", "wrongid", "hang", "die"} {
		mode.Store(m)
		// Рабочий сломался — этот запрос делает разовый.
		if how, err := call(); how != "разовый" || err != nil {
			t.Fatalf("%s: %s %v", m, how, err)
		}
		// Сразу после падения не пересоздаём (не плодим процессы).
		mode.Store("ok")
		if how, _ := call(); how != "разовый" || int(f.starts.Load()) != i+1 {
			t.Fatalf("%s: %s, запусков %d", m, how, f.starts.Load())
		}
		// Через RestartGap — новый процесс.
		*now = now.Add(RestartGap)
		if how, _ := call(); how != "рабочий" || int(f.starts.Load()) != i+2 {
			t.Fatalf("%s: %s, запусков %d", m, how, f.starts.Load())
		}
	}
	if len(*logs) == 0 || !strings.Contains(strings.Join(*logs, "\n"), "fake stderr") {
		t.Errorf("в журнале нет хвоста stderr: %q", *logs)
	}
	// MaxStartFails падений подряд — только разовый, процессов больше нет.
	mode.Store("die")
	for i := 0; i < MaxStartFails; i++ {
		call()
		*now = now.Add(RestartGap)
	}
	n := f.starts.Load()
	mode.Store("ok")
	*now = now.Add(time.Hour)
	if how, _ := call(); how != "разовый" || f.starts.Load() != n {
		t.Fatalf("%s, запусков %d → %d", how, n, f.starts.Load())
	}
}

func TestWorkerStartFails(t *testing.T) {
	// Не ответил «готов» — разовый; ответил ошибкой WinRT — тоже.
	for _, ready := range []func(int) string{
		func(int) string { return "" },
		func(int) string { return reply(0, `{"error":"Class not registered"}`) },
		func(int) string { return "EXIT" },
	} {
		f := &fakeShell{ready: ready, handle: func(id int, r Request) string { return reply(id, `{"items":[]}`) }}
		w, _, _ := testWorker(f)
		t0 := time.Now()
		if _, how, err := w.Call(context.Background(), Request{Cmd: "langs"}, func() ([]byte, error) { return nil, nil }); how != "разовый" || err != nil {
			t.Fatal(how, err)
		}
		if time.Since(t0) > time.Second {
			t.Fatal("запрос ждал запуск слишком долго")
		}
	}
	// Запуск дольше waitStart: запрос не ждёт, уходит в разовый; запуск
	// продолжается, и следующий запрос идёт в рабочего.
	release := make(chan struct{})
	f := &fakeShell{ready: func(int) string { <-release; return reply(0, `{"ready":true}`) },
		handle: func(id int, r Request) string { return reply(id, `{"items":[]}`) }}
	w, _, _ := testWorker(f)
	w.waitStart = 50 * time.Millisecond
	if _, how, _ := w.Call(context.Background(), Request{Cmd: "langs"}, func() ([]byte, error) { return nil, nil }); how != "разовый" {
		t.Fatal(how)
	}
	close(release)
	w.Warm()
	if _, how, _ := w.Call(context.Background(), Request{Cmd: "langs"}, func() ([]byte, error) { return nil, nil }); how != "рабочий" || f.starts.Load() != 1 {
		t.Fatal(how, f.starts.Load())
	}
	w.Stop()
}

// I3: ошибка скрипта внутри рабочего — этот запрос делает разовый скрипт;
// справился разовый — это беда рабочего (убить, считать), и после
// MaxStartFails таких раз — только разовый. Разовый тоже не справился
// (например, AUMID не принят) — отдаём ошибку рабочего, рабочий живёт.
func TestWorkerScriptErrorFallsBack(t *testing.T) {
	f := &fakeShell{ready: okReady, handle: func(id int, r Request) string {
		return reply(id, `{"items":[{"kind":"error","msg":"Method invocation failed"}]}`)
	}}
	w, logs, now := testWorker(f)
	good := func() ([]byte, error) { return []byte("{\"kind\":\"ocr\",\"lang\":\"ru\",\"lines\":[\"x\"]}\n"), nil }
	bad := func() ([]byte, error) { return []byte("AUMID\n"), errors.New("exit status 1") }

	out, how, err := w.Call(context.Background(), Request{Cmd: "toast"}, bad)
	if how != "рабочий" || err == nil || err.Error() != "Method invocation failed" || f.starts.Load() != 1 {
		t.Fatal(how, err, f.starts.Load())
	}
	if _, how, _ := w.Call(context.Background(), Request{Cmd: "toast"}, bad); how != "рабочий" || f.starts.Load() != 1 {
		t.Fatal("ошибка общая для обоих путей — рабочий не виноват", how, f.starts.Load())
	}
	for i := 0; i < MaxStartFails; i++ {
		out, how, err = w.Call(context.Background(), Request{Cmd: "ocr", Langs: []string{"ru"}}, good)
		if how != "разовый" || err != nil || len(parse(out).Lines["ru"]) != 1 {
			t.Fatalf("%d: %s %v %q", i, how, err, out)
		}
		*now = now.Add(RestartGap)
	}
	n := f.starts.Load()
	if n != MaxStartFails {
		t.Fatalf("запусков %d", n)
	}
	*now = now.Add(time.Hour)
	if _, how, _ := w.Call(context.Background(), Request{Cmd: "ocr"}, good); how != "разовый" || f.starts.Load() != n {
		t.Fatalf("после %d ошибок — только разовый: %s, запусков %d", MaxStartFails, how, f.starts.Load())
	}
	if !strings.Contains(strings.Join(*logs, "\n"), "разовый справился") {
		t.Errorf("%q", *logs)
	}
}

// Отмена запроса вызывающим — не беда рабочего (M4); Live — рабочий готов.
func TestWorkerCancelAndLive(t *testing.T) {
	f := &fakeShell{ready: okReady, handle: func(id int, r Request) string {
		if r.Cmd == "hang" {
			return ""
		}
		return reply(id, `{"items":[]}`)
	}}
	w, _, _ := testWorker(f)
	if w.Live() {
		t.Fatal("ещё не запущен")
	}
	w.Warm()
	if !w.Live() {
		t.Fatal("запущен")
	}
	for i := 0; i < MaxStartFails+1; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		w.Do(ctx, Request{Cmd: "hang"})
		cancel()
	}
	if _, err := w.Do(context.Background(), Request{Cmd: "langs"}); err != nil {
		t.Fatalf("отмены не считаются неудачами: %v", err)
	}
	w.Stop()
	if w.Live() {
		t.Fatal("остановлен")
	}
}
