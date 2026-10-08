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
func OnExit(s settings.Settings, ending, installing bool) bool {
	return Wanted(s) && !ending && !installing
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

// HowToLaunch: admin — сторож администратор; fromMarker — этого сторожа
// уже подняла задача по просьбе сторожа без прав, а прав так и нет (по
// второму кругу задачу не зовём — иначе по кругу); task — задача есть.
func HowToLaunch(admin, fromMarker, task bool) Way {
	switch {
	case admin:
		return Direct
	case task && !fromMarker:
		return ViaTask
	}
	return Ask
}

// LaunchArgs — аргументы программы, которую поднимает сторож: без окна (в
// трей), игра только что запустилась.
func LaunchArgs() []string { return []string{autostart.Flag, "-" + FromFlag} }

// MarkerFile — метка «игра запустилась, подними программу»: её пишет сторож
// без прав перед schtasks /Run, читает тот, кого подняла задача (сторож
// или программа).
const MarkerFile = "watch-game.start"

// MarkerTTL — сколько метка свежа.
const MarkerTTL = 2 * time.Minute

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
	// Release снимает знак (до запуска программы: задача может поднять
	// нового сторожа).
	Release()
}

// Env — то, что зависит от Windows и настроек; в тестах подделки.
type Env struct {
	Acquire func() (Guard, bool) // false — сторож уже запущен
	Wanted  func() bool          // настройки: сторож нужен
	Marker  func() bool          // TakeMarker: подняли по просьбе сторожа без прав
	Running func() (bool, error) // есть ли игра
	Launch  func(fromMarker bool) error
	Every   time.Duration // 0 — Every
	Logf    func(format string, a ...any)
}

// Result — чем кончилась работа сторожа.
type Result int

const (
	Busy      Result = iota // другой сторож уже следит
	NotWanted               // в настройках сторож не нужен
	Stopped                 // попросили закрыться (программа запустилась сама)
	Launched                // игра запустилась, программа поднята
	Failed                  // игра запустилась, программу поднять не вышло
)

func (r Result) String() string {
	return [...]string{"уже запущен другой сторож", "в настройках не нужен", "закрыт по просьбе",
		"программа поднята", "программу поднять не вышло"}[r]
}

// Serve — весь сторож: одна копия, снимки до запуска игры, запуск
// программы один раз и выход.
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
	released := false
	release := func() {
		if !released {
			released = true
			g.Release()
		}
	}
	defer release()

	fromMarker := e.Marker != nil && e.Marker()
	if !fromMarker && e.Wanted != nil && !e.Wanted() {
		logf("«вместе с игрой» выключено — сторож не нужен")
		return NotWanted
	}
	if !fromMarker {
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
	} else {
		logf("подняла задача Планировщика по просьбе сторожа — поднимаю программу")
	}
	release()
	if err := e.Launch(fromMarker); err != nil {
		logf("программа не поднялась: %v", err)
		return Failed
	}
	return Launched
}
