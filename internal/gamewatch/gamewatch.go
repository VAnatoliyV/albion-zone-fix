// Пакет gamewatch — сторож игры (как СторожИгры у мака): заметить, что
// клиент Albion запустился или закрылся, и решить, что делать программе —
// показать окно, начать сбор цен, закрыться.
//
// На маке система сама сообщает о запуске и закрытии программ. На Windows
// без подписок WMI так не выйдет, поэтому раз в несколько секунд берём
// снимок процессов (toolhelp32) и сравниваем с прошлым.
package gamewatch

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Names — exe клиента игры (без учёта регистра). Лаунчер нарочно не
// считаем, как и мак: он открывается и закрывается отдельно от игры.
var Names = []string{"Albion-Online.exe"}

// IsGame — имя exe — клиент игры.
func IsGame(exe string) bool {
	for _, n := range Names {
		if strings.EqualFold(exe, n) {
			return true
		}
	}
	return false
}

// ErrUnsupported — снимок процессов не умеем (не Windows).
var ErrUnsupported = errors.New("снимок процессов есть только на Windows")

// Event — что случилось с игрой между двумя снимками.
type Event int

const (
	None    Event = iota
	Started       // игры не было — появилась
	Exited        // игра была — пропала
)

func (e Event) String() string {
	switch e {
	case Started:
		return "игра запустилась"
	case Exited:
		return "игра закрылась"
	}
	return "—"
}

// ExitPolls — сколько снимков подряд игры не должно быть, чтобы считать её
// закрытой: один промах (снимок сделан в неудачный миг) не закрывает
// программу.
const ExitPolls = 2

// ShowDelay — через сколько после запуска игры выводить окно вперёд:
// снимок замечает процесс раньше, чем появляется окно игры, и показанное
// сразу окно тут же оказалось бы под ним.
const ShowDelay = 8 * time.Second

// Watcher решает по последовательности снимков. Первый снимок — только
// точка отсчёта: игра, запущенная до программы, «запуском» не считается
// (мак тоже узнаёт лишь о запусках после себя), а её закрытие — считается.
type Watcher struct {
	known   bool // был хоть один снимок
	running bool // игра есть (по последнему решению)
	missing int  // снимков подряд без игры, пока она считается запущенной
}

// Step — очередной снимок: есть ли игра.
func (w *Watcher) Step(present bool) Event {
	if !w.known {
		w.known, w.running = true, present
		return None
	}
	if present {
		w.missing = 0
		if !w.running {
			w.running = true
			return Started
		}
		return None
	}
	if !w.running {
		return None
	}
	w.missing++
	if w.missing < ExitPolls {
		return None
	}
	w.running, w.missing = false, 0
	return Exited
}

// Reset забывает прошлое: следующий снимок снова станет точкой отсчёта
// (слежку выключили и включили, пока игра шла, — это не запуск).
func (w *Watcher) Reset() { *w = Watcher{} }

// Options — что включено в настройках «вместе с игрой».
type Options struct {
	Show    bool // показать окно, когда игра запустилась
	Collect bool // начать сбор цен, когда игра запустилась
	Quit    bool // закрыться, когда игра закрылась
}

// Actions — что сделать программе.
type Actions struct {
	Show, Collect, Quit bool
}

// Decide — что делать по событию. collecting — сбор уже идёт (тогда
// запускать нечего).
func Decide(e Event, o Options, collecting bool) Actions {
	switch e {
	case Started:
		return Actions{Show: o.Show, Collect: o.Collect && !collecting}
	case Exited:
		return Actions{Quit: o.Quit}
	}
	return Actions{}
}

// Config — сторожу от программы.
type Config struct {
	Every   time.Duration        // как часто снимать процессы; 0 — 3 с
	Options func() Options       // текущие настройки; все выключены — не следим
	Running func() (bool, error) // есть ли игра; nil — Running пакета
	On      func(Event)          // запуск или закрытие игры
}

// Run следит, пока не отменят ctx. Ошибка снимка — снимок пропускается.
func Run(ctx context.Context, c Config) {
	if c.Every == 0 {
		c.Every = 3 * time.Second
	}
	if c.Running == nil {
		c.Running = Running
	}
	var w Watcher
	t := time.NewTicker(c.Every)
	defer t.Stop()
	for {
		o := c.Options()
		if !o.Show && !o.Collect && !o.Quit {
			w.Reset()
		} else if present, err := c.Running(); err == nil {
			if e := w.Step(present); e != None && c.On != nil {
				c.On(e)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
