package pwsh

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// Proc — запущенный рабочий PowerShell: стандартный ввод (Write), вывод
// (Read), убить, хвост stderr для журнала. На Windows — powershell.exe, в
// тестах — подделка.
type Proc interface {
	Write(p []byte) (int, error)
	Read(p []byte) (int, error)
	Kill()
	Tail() string
}

// ErrOff — рабочего сейчас нет (ждём RestartGap после падения, сдались
// после MaxStartFails падений или программа выходит): только разовый режим.
var ErrOff = errors.New("рабочий PowerShell выключен")

var (
	errDied    = errors.New("процесс завершился")
	errTimeout = errors.New("не ответил за отведённое время")
)

// Worker — один рабочий PowerShell, запросы по одному. Запуск идёт в
// своей горутине: запрос ждёт его не дольше waitStart, а дальше уходит в
// разовый PowerShell — зависший запуск не держит нажатие.
type Worker struct {
	start func() (Proc, error)
	logf  func(string, ...any)

	now                                             func() time.Time
	readyTimeout, reqTimeout, restartGap, waitStart time.Duration
	maxFails                                        int

	reqMu    sync.Mutex // один запрос за раз
	mu       sync.Mutex // состояние ниже
	p        Proc
	lines    <-chan string
	stop     chan struct{}
	starting chan struct{} // идёт запуск; закроется по его концу
	id       int
	fails    int // неудач подряд (запуск или запрос)
	lastFail time.Time
	off      bool // сдались: только разовый режим
	stopped  bool // программа выходит
}

// WaitStart — сколько запрос ждёт идущий запуск рабочего (обычно он
// готов за 1–1.5 с), потом — разовый PowerShell.
const WaitStart = 3 * time.Second

// NewWorker — рабочий с запуском start (процесс поднимается Warm или
// первым запросом).
func NewWorker(start func() (Proc, error), logf func(string, ...any)) *Worker {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Worker{start: start, logf: logf, now: time.Now, readyTimeout: ReadyTimeout, reqTimeout: ReqTimeout,
		restartGap: RestartGap, waitStart: WaitStart, maxFails: MaxStartFails}
}

// Warm запускает рабочего заранее и ждёт конца запуска (звать в горутине).
func (w *Worker) Warm() {
	if ch := w.kick(); ch != nil {
		<-ch
	}
}

// Stop убивает рабочего навсегда (выход программы).
func (w *Worker) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopped = true
	w.killLocked()
}

var closed = func() chan struct{} { c := make(chan struct{}); close(c); return c }()

// kick — рабочий есть (закрытый канал), запускается (канал запуска) или
// запустить нельзя (nil: ErrOff).
func (w *Worker) kick() <-chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch {
	case w.p != nil:
		return closed
	case w.starting != nil:
		return w.starting
	case w.stopped || w.off:
		return nil
	case !w.lastFail.IsZero() && w.now().Sub(w.lastFail) < w.restartGap:
		return nil
	}
	ch := make(chan struct{})
	w.starting = ch
	go w.launch(ch)
	return ch
}

// launch — запуск и ожидание готовности (без w.mu).
func (w *Worker) launch(ch chan struct{}) {
	defer close(ch)
	t0 := time.Now()
	p, err := w.start()
	var (
		lines chan string
		stop  chan struct{}
	)
	if err == nil {
		lines, stop = make(chan string, 16), make(chan struct{})
		go readLines(p, lines, stop)
		var body []byte
		body, err = await(context.Background(), lines, 0, w.readyTimeout)
		if err == nil {
			err = readyReply(body)
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.starting = nil
	if err != nil {
		tail := ""
		if p != nil {
			tail = p.Tail()
			close(stop)
			p.Kill()
		}
		w.failLocked("запуск", err, tail)
		return
	}
	if w.stopped {
		close(stop)
		p.Kill()
		return
	}
	w.p, w.lines, w.stop = p, lines, stop
	w.logf("рабочий PowerShell запущен за %v", time.Since(t0).Round(time.Millisecond))
}

// Do выполняет запрос в рабочем; ответ — строки JSON, как у разового
// скрипта. Ошибка — только беда самого рабочего (его уже убили и записали в
// журнал), ErrOff или «ещё запускается»; ошибку внутри скрипта ищи в ответе
// (ErrorMsg).
func (w *Worker) Do(ctx context.Context, req Request) ([]byte, error) {
	w.reqMu.Lock()
	defer w.reqMu.Unlock()
	ch := w.kick()
	if ch == nil {
		return nil, ErrOff
	}
	t := time.NewTimer(w.waitStart)
	select {
	case <-ch:
		t.Stop()
	case <-t.C:
		return nil, errStarting
	case <-ctx.Done():
		t.Stop()
		return nil, ctx.Err()
	}
	w.mu.Lock()
	p, lines := w.p, w.lines
	w.id++
	id := w.id
	w.mu.Unlock()
	if p == nil {
		return nil, ErrOff // запуск не удался (уже в журнале)
	}
	line, err := encodeRequest(id, req)
	if err != nil {
		return nil, err
	}
	if _, err := io.WriteString(p, line); err != nil {
		w.fail(p, req.Cmd, fmt.Errorf("запись запроса: %v", err))
		return nil, err
	}
	body, err := await(ctx, lines, id, w.reqTimeout)
	var out []byte
	if err == nil {
		out, err = itemsOut(body)
	}
	if err != nil {
		w.fail(p, req.Cmd, err)
		return nil, err
	}
	w.mu.Lock()
	w.fails = 0
	w.mu.Unlock()
	return out, nil
}

var errStarting = errors.New("рабочий PowerShell ещё запускается")

// await ждёт ответ на запрос id; строки без маркера пропускает.
func await(ctx context.Context, lines <-chan string, id int, timeout time.Duration) ([]byte, error) {
	t := time.NewTimer(timeout)
	defer t.Stop()
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				return nil, errDied
			}
			got, body, isReply, err := parseReply(line)
			if !isReply {
				continue
			}
			if err != nil {
				return nil, err
			}
			if got != id {
				return nil, fmt.Errorf("ответ не на тот запрос: %d вместо %d", got, id)
			}
			return body, nil
		case <-t.C:
			return nil, errTimeout
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// fail — беда рабочего p во время запроса: убить, в журнал.
func (w *Worker) fail(p Proc, what string, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.p != p {
		return // уже убит (выход программы)
	}
	tail := p.Tail()
	w.killLocked()
	w.failLocked(what, err, tail)
}

// failLocked — считать неудачу и записать в журнал (под w.mu).
func (w *Worker) failLocked(what string, err error, tail string) {
	w.lastFail = w.now()
	w.fails++
	msg := fmt.Sprintf("рабочий PowerShell (%s): %v", what, err)
	if tail = strings.TrimSpace(tail); tail != "" {
		if r := []rune(tail); len(r) > 300 {
			tail = "…" + string(r[len(r)-300:])
		}
		msg += "; stderr: " + tail
	}
	if w.stopped {
		return
	}
	if w.fails >= w.maxFails {
		w.off = true
		w.logf("%s — %d неудач подряд, дальше только разовый PowerShell", msg, w.fails)
		return
	}
	w.logf("%s — убит, пересоздам не раньше чем через %v (пока разовый PowerShell)", msg, w.restartGap)
}

func (w *Worker) killLocked() {
	if w.p == nil {
		return
	}
	close(w.stop)
	w.p.Kill()
	w.p, w.lines, w.stop = nil, nil, nil
}

// readLines — строки вывода рабочего в канал; закрывает его на конце вывода.
func readLines(r io.Reader, out chan<- string, stop <-chan struct{}) {
	defer close(out)
	br := bufio.NewReaderSize(r, 64*1024)
	for {
		s, err := br.ReadString('\n')
		if s = strings.TrimRight(s, "\r\n"); s != "" {
			select {
			case out <- s:
			case <-stop:
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// Call — запрос через рабочего; рабочего нет или он не справился — тем же
// скриптом, что раньше, разовым PowerShell (oneShot). how — «рабочий» или
// «разовый» (для журнала). err — ошибка рабочего/скрипта, как у RunScript.
func (w *Worker) Call(ctx context.Context, req Request, oneShot func() ([]byte, error)) (out []byte, how string, err error) {
	out, err = w.Do(ctx, req)
	if err == nil {
		if m := ErrorMsg(out); m != "" {
			err = errors.New(m)
		}
		return out, "рабочий", err
	}
	if ctx.Err() != nil {
		return nil, "рабочий", ctx.Err()
	}
	w.logf("%s: разовый PowerShell (%v)", req.Cmd, err)
	out, err = oneShot()
	return out, "разовый", err
}

// Общий рабочий программы.
var (
	sharedOnce sync.Once
	shared     *Worker
	logMu      sync.Mutex
	sharedLog  = func(string, ...any) {}
)

func sharedWorker() *Worker {
	sharedOnce.Do(func() {
		shared = NewWorker(startProc, func(f string, a ...any) {
			logMu.Lock()
			lf := sharedLog
			logMu.Unlock()
			lf(f, a...)
		})
	})
	return shared
}

// Warm — запустить общего рабочего заранее (в своей горутине); logf —
// журнал. На маке ничего не делает.
func Warm(logf func(string, ...any)) {
	if logf != nil {
		logMu.Lock()
		sharedLog = logf
		logMu.Unlock()
	}
	if !supported {
		return
	}
	go sharedWorker().Warm()
}

// Stop — убить общего рабочего (выход программы; объект задания тоже
// гасит его, даже при аварийном выходе).
func Stop() { sharedWorker().Stop() }

// Call — запрос req через общего рабочего, а не вышло — разовым PowerShell
// со скриптом script и переменными env.
func Call(ctx context.Context, req Request, script string, env []string) ([]byte, string, error) {
	return sharedWorker().Call(ctx, req, func() ([]byte, error) { return RunScript(ctx, script, env) })
}
