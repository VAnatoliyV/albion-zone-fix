// Пакет gameguard — сторож игры вне программы (как сторож-игры у мака):
// «Открывать программу при запуске Albion» и «Включать сбор при запуске
// Albion» работают и тогда, когда сама программа закрыта.
//
// Сторож — тот же AlbionJournal.exe с ключом -watch-game: без окна,
// WebView2, WinDivert и PowerShell. Раз в несколько секунд снимок процессов
// (как internal/gamewatch); появилась игра — поднять программу и выйти.
//
// Кто поднимает сторожа: сама программа при выходе (кроме выхода ради
// обновления и выключения Windows) и задача Планировщика при входе в
// Windows, если «Запускать вместе с Windows» выключен (иначе при входе
// поднимается сама программа — двух процессов не нужно). Программа при
// старте закрывает работающего сторожа.
//
// Права: сторож, поднятый программой (администратор) или задачей с
// наивысшими правами, сам администратор и поднимает программу без окна
// UAC. Сторож без прав просит задачу Планировщика (schtasks /Run) — окно
// UAC посреди игры хуже всего.
package gameguard

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"albionzonefix/internal/autostart"
	"albionzonefix/internal/gamewatch"
	"albionzonefix/internal/settings"
)

// Flag — ключ запуска сторожа (имя флага без минуса).
const Flag = "watch-game"

// FromFlag — ключ, с которым сторож поднимает программу: игра только что
// запустилась (программа сразу показывает окно и включает сбор по
// настройкам, а не ждёт следующего запуска игры).
const FromFlag = "from-watch"

// StopFlag — закрыть работающего сторожа (для установщика). Код выхода:
// 0 — сторож был и закрыт, 1 — сторожа не было, 2 — не закрылся.
const StopFlag = "stop-watch"

// Every — как часто сторож снимает процессы.
const Every = 2500 * time.Millisecond

// Wanted — нужен ли сторож: включено «открывать» или «включать сбор» при
// запуске игры. «Закрываться вместе с Albion» сторожу не нужно — закрытой
// программе закрываться нечего.
func Wanted(s settings.Settings) bool { return s.ShowWithGame || s.StartWithGame }

// OnExit — оставить ли сторожа при выходе программы. Не оставляем, когда
// ставится обновление (сторож — тот же exe, он мешал бы замене; поднимет
// установщик) и когда выключается Windows.
// quitAll — «Выйти совсем» в трее: в этот раз без сторожа.
func OnExit(s settings.Settings, ending, installing, quitAll bool) bool {
	return Wanted(s) && !ending && !installing && !quitAll
}

// TaskMode — что должна запускать задача Планировщика при входе в Windows.
func TaskMode(s settings.Settings) autostart.Mode {
	return autostart.ModeFor(s.StartWithWindows, Wanted(s))
}

// Way — как сторожу поднять программу.
type Way int

const (
	Direct  Way = iota // напрямую, с правами сторожа (сторож — администратор)
	ViaTask            // через задачу Планировщика: она даст права без окна UAC
	Ask                // напрямую без прав: программа сама спросит права (UAC)
)

func (w Way) String() string {
	switch w {
	case Direct:
		return "напрямую"
	case ViaTask:
		return "через задачу Планировщика"
	}
	return "напрямую с запросом прав"
}

// HowToLaunch: admin — сторож администратор; task — что запускает задача
// при входе. Через задачу — только если она запускает программу: задача
// сторожа — это, скорее всего, сам этот сторож, и /Run при IgnoreNew
// пропал бы молча (да и прав она бы не дала — раз их нет у сторожа).
func HowToLaunch(admin bool, task autostart.Mode) Way {
	switch {
	case admin:
		return Direct
	case task == autostart.App:
		return ViaTask
	}
	return Ask
}

// LaunchViaTask — метка «игра запустилась» и schtasks /Run (run): задача
// поднимет программу, та заберёт метку. /Run не вышел — метку убрать, чтобы
// ручной запуск не принял её за запуск игры.
func LaunchViaTask(dir string, now time.Time, run func() error) error {
	if err := WriteMarker(dir, now); err != nil {
		return err
	}
	if err := run(); err != nil {
		os.Remove(filepath.Join(dir, MarkerFile))
		return err
	}
	return nil
}

// LaunchArgs — аргументы программы, которую поднимает сторож: без окна (в
// трей), игра только что запустилась.
func LaunchArgs() []string { return []string{autostart.Flag, "-" + FromFlag} }

// MarkerFile — метка «игра запустилась, подними программу»: её пишет сторож
// без прав перед schtasks /Run, читает программа, которую подняла задача.
const MarkerFile = "watch-game.start"

// MarkerTTL — сколько метка свежа: задача поднимает программу за секунды,
// а ручной запуск позже не должен принять старую метку за запуск игры.
const MarkerTTL = 30 * time.Second

// WriteMarker пишет метку с текущим временем.
func WriteMarker(dir string, now time.Time) error {
	return os.WriteFile(filepath.Join(dir, MarkerFile), []byte(strconv.FormatInt(now.Unix(), 10)), 0644)
}

// TakeMarker забирает метку: была и свежая — true. Метку удаляет в любом
// случае.
func TakeMarker(dir string, now time.Time) bool {
	p := filepath.Join(dir, MarkerFile)
	b, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	os.Remove(p)
	sec, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return false
	}
	d := now.Sub(time.Unix(sec, 0))
	return d >= -time.Minute && d <= MarkerTTL
}

// Guard — знак «сторож уже запущен» и просьба закрыться.
type Guard interface {
	// Wait ждёт d; true — сторожа просят закрыться.
	Wait(d time.Duration) bool
	// Release снимает знак.
	Release()
}

// Env — то, что зависит от Windows и настроек; в тестах подделки.
type Env struct {
	Acquire func() (Guard, bool) // false — сторож уже запущен
	Wanted  func() bool          // настройки: сторож нужен
	Running func() (bool, error) // есть ли игра
	// AppRunning — программа запущена (её знак одной копии). nil — не знаем.
	AppRunning func() bool
	// AppWait — сколько ждать выхода программы, которая ещё закрывается
	// (0 — AppWaitDefault).
	AppWait time.Duration
	Launch  func() error
	Every   time.Duration // 0 — Every
	Logf    func(format string, a ...any)
}

// AppWaitDefault — сколько сторож ждёт, пока программа, оставившая его при
// выходе, доделает выход (сохранение сессии, остановка сбора).
const AppWaitDefault = 15 * time.Second

// Result — чем кончилась работа сторожа.
type Result int

const (
	Busy      Result = iota // другой сторож уже следит
	NotWanted               // в настройках сторож не нужен
	Stopped                 // попросили закрыться (программа запустилась сама)
	Launched                // игра запустилась, программа поднята
	Failed                  // игра запустилась, программу поднять не вышло
	AppAlive                // игра запустилась, а программа и так запущена
)

func (r Result) String() string {
	return [...]string{"уже запущен другой сторож", "в настройках не нужен", "закрыт по просьбе",
		"программа поднята", "программу поднять не вышло", "программа и так запущена"}[r]
}

// Serve — весь сторож: одна копия, снимки до запуска игры, запуск
// программы один раз и выход. Знак одной копии держится до конца: пока
// сторож поднимает программу, StopRunning его видит.
func Serve(e Env) Result {
	logf := e.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	every := e.Every
	if every == 0 {
		every = Every
	}
	g, ok := e.Acquire()
	if !ok {
		logf("другой сторож уже следит — выхожу")
		return Busy
	}
	defer g.Release()

	if e.Wanted != nil && !e.Wanted() {
		logf("«вместе с игрой» выключено — сторож не нужен")
		return NotWanted
	}
	logf("жду запуска игры")
	// Первый снимок — точка отсчёта: игра, которая уже шла, когда
	// программу закрыли руками, — не повод её поднимать.
	var w gamewatch.Watcher
	var lastErr string
	for {
		present, err := e.Running()
		if err == nil {
			if w.Step(present) == gamewatch.Started {
				logf("игра запустилась — поднимаю программу")
				break
			}
		} else if err.Error() != lastErr {
			lastErr = err.Error()
			logf("снимок процессов: %v", err)
		}
		if g.Wait(every) {
			logf("программа запустилась сама — сторож больше не нужен")
			return Stopped
		}
	}
	// Программа, оставившая сторожа, могла ещё не доделать выход: вторая
	// копия показала бы сообщение «уже запущена» поверх игры.
	if e.AppRunning != nil && e.AppRunning() {
		wait := e.AppWait
		if wait == 0 {
			wait = AppWaitDefault
		}
		step := min(250*time.Millisecond, wait)
		for waited := time.Duration(0); e.AppRunning(); waited += step {
			if waited >= wait {
				logf("программа и так запущена — она следит за игрой сама")
				return AppAlive
			}
			if g.Wait(step) {
				logf("программа запустилась сама — сторож больше не нужен")
				return Stopped
			}
		}
	}
	// Просьба закрыться могла прийти между снимком и запуском.
	if g.Wait(0) {
		logf("программа запустилась сама — сторож больше не нужен")
		return Stopped
	}
	if err := e.Launch(); err != nil {
		logf("программа не поднялась: %v", err)
		return Failed
	}
	return Launched
}

// SyncTask приводит задачу при входе к новым настройкам (sync —
// autostart.Sync). Режим не менялся — ничего. Ошибка возвращается всегда
// (страница её покажет); rollback — откатить «Запускать вместе с Windows»
// (без задачи эта настройка не работает вовсе, а «вместе с игрой» без
// задачи сторожа работает, пока не перезагрузишься).
func SyncTask(old, cur settings.Settings, sync func(autostart.Mode) error) (rollback bool, err error) {
	if TaskMode(old) == TaskMode(cur) {
		return false, nil
	}
	if err := sync(TaskMode(cur)); err != nil {
		return old.StartWithWindows != cur.StartWithWindows, err
	}
	return false, nil
}

// stopOps — то, на чём держится StopRunning; в тестах подделки.
type stopOps struct {
	running func() bool // знак сторожа есть
	signal  func() bool // попросить закрыться; false — события ещё нет
	// open — процесс сторожа: функция ожидания его выхода; false — ещё не
	// узнать (сторож только запускается).
	open  func() (func(time.Duration) bool, bool)
	sleep func(time.Duration)
	now   func() time.Time
}

// stop: просьба повторяется, пока событие не появится (сторож мог взять
// знак, но ещё не создать событие), и ждём выхода самого процесса — знак
// снимается раньше, чем Windows отпускает exe.
func stop(o stopOps, wait time.Duration) (was, ok bool) {
	if !o.running() {
		return false, true
	}
	deadline := o.now().Add(wait)
	signaled := false
	var waitProc func(time.Duration) bool
	for {
		if !signaled {
			signaled = o.signal()
		}
		if waitProc == nil {
			if f, ok := o.open(); ok {
				waitProc = f
			}
		}
		left := deadline.Sub(o.now())
		if signaled && waitProc != nil {
			if left < 0 {
				left = 0
			}
			return true, waitProc(left)
		}
		if waitProc == nil && !o.running() {
			return true, true // вышел сам, процесс уже не открыть
		}
		if left <= 0 {
			return true, false
		}
		o.sleep(50 * time.Millisecond)
	}
}
