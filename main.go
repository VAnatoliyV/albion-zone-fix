// Albion Journal для Windows — сбор цен, счётчик фейма и урона и Zone Fix
// (статистика переходов между локациями Albion Online и обход чёрного экрана
// для игроков, у которых провайдер портит UDP игры).
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"albionzonefix/internal/app"
	"albionzonefix/internal/autostart"
	"albionzonefix/internal/collector"
	"albionzonefix/internal/datadir"
	"albionzonefix/internal/desktop"
	"albionzonefix/internal/game"
	"albionzonefix/internal/i18n"
	"albionzonefix/internal/names"
	"albionzonefix/internal/receiver"
	"albionzonefix/internal/record"
	"albionzonefix/internal/settings"
	"albionzonefix/internal/sniff"
	"albionzonefix/internal/support"
	"albionzonefix/internal/ui"
	"albionzonefix/internal/update"
)

// version — версия сборки; собрать.sh ставит её через -ldflags "-X main.version=…".
// «dev» — сборка разработчика: автообновление выключено.
var version = "dev"

// uiFile — адрес и ключ страницы работающей копии: по ним вторая копия
// просит первую показать окно.
const uiFile = "ui.json"

func main() {
	replay := flag.String("replay", "", "прогнать запись .azf и напечатать переходы")
	hidden := flag.Bool("autostart", false, "запуск вместе с Windows: без окна, сразу в трей")
	showVersion := flag.Bool("version", false, "напечатать версию и выйти")
	apply := flag.String(update.ApplyFlag, "", "поставить обновление из папки (запускает сама программа)")
	applyTarget := flag.String("target", "", "для -apply-update: папка программы")
	applyPrev := flag.String("prev", "", "для -apply-update: куда убрать прежние файлы")
	applyData := flag.String("data", "", "для -apply-update: каталог данных")
	applyPID := flag.Int("pid", 0, "для -apply-update: чьего выхода ждать")
	applyRestart := flag.Bool("restart", false, "для -apply-update: запустить программу после установки")
	flag.Parse()

	// Версию спрашивает старая копия у скачанной новой — до всего остального
	// (без прав администратора, без окна и без проверки второй копии).
	if *showVersion {
		fmt.Println(version)
		return
	}
	if *apply != "" {
		os.Exit(runApply(update.Plan{Src: *apply, Dest: *applyTarget, Prev: *applyPrev,
			PID: *applyPID, Restart: *applyRestart, DataDir: *applyData}))
	}

	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	if *replay != "" {
		os.Exit(runReplay(*replay))
	}

	data, dataErr := datadir.Dir()
	if dataErr != nil {
		data = dir
	}
	lang := i18n.Resolve(settings.Open(data).Get().Language, i18n.System())

	// Одна копия: вторая (ручной запуск поверх автозапуска) показывает окно
	// первой и выходит. Без прав администратора знак только проверяем:
	// создаст его копия, перезапущенная с правами.
	if desktop.OtherRunning() {
		showOther(data, lang)
		return
	}
	if !isAdmin() {
		if err := relaunchAsAdmin(); err != nil {
			desktop.Message("Albion Journal", i18n.T(lang, "msg.needAdmin"))
		}
		return
	}
	if !desktop.SingleInstance() {
		showOther(data, lang)
		return
	}

	// Журнал программы, сборщика и библиотек окна. Консоли нет
	// (-H windowsgui), поэтому всё, что раньше печаталось, идёт сюда.
	logPath := filepath.Join(data, "albion-journal.log")
	logf, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	var logw io.Writer = io.Discard
	if err == nil {
		logw = logf
		defer logf.Close()
	}
	log.SetOutput(logw)
	logLine := func(format string, args ...any) {
		fmt.Fprintf(logw, "[программа] %s %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
	}
	logLine("запуск Albion Journal %s, данные: %s", version, data)
	if dataErr != nil {
		logLine("нет каталога данных, пишу рядом с программой: %v", dataErr)
	}

	binDir := filepath.Join(dir, "zapret", "bin")
	if moved, err := datadir.Migrate(dir, data); len(moved) > 0 || err != nil {
		logLine("перенёс файлы Zone Fix в %s: %v %v", data, moved, err)
	}
	a := app.New(data, binDir, names.Zones())

	// Приёмник своих цен (acp-prices.exe рядом с программой): стартует вместе
	// со сбором, вывод идёт в тот же журнал.
	var rlog io.Writer = io.Discard
	if logf != nil {
		rlog = logf
	}
	rcv := receiver.NewManager(filepath.Join(dir, receiver.ExeName), data, rlog)
	a.AttachReceiver(rcv)

	// Разборщик сборщика цен: журнал — общий, таблица предметов — рядом с exe.
	col := collector.New(data, dir, logw)
	if err := a.AttachCollector(col); err != nil {
		logLine("сборщик цен не запустился: %v", err)
	}

	// Автообновление: выпуски GitHub, подпись ed25519 (internal/update).
	upd := update.New(update.Config{
		Dir: filepath.Join(data, "обновление"), Current: version, ProgramDir: dir, DataDir: data,
		Auto: func() bool { return a.Settings().AutoUpdate },
		Logf: func(format string, args ...any) {
			fmt.Fprintf(logw, "[обновление] %s %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
		},
	})
	upd.Start()
	defer upd.Stop()

	// Всё останавливаем один раз: при выходе из трея, при выключении Windows
	// (окно получает WM_ENDSESSION) и по Ctrl+C в разработке.
	var stopOnce sync.Once
	shutdown := func() {
		stopOnce.Do(func() {
			logLine("выход: останавливаю сбор, счётчик и обход")
			// Всё, что живёт внутри программы, останавливается всегда;
			// приёмник — только при «останавливать всё при выходе».
			a.Shutdown()
			// Ставится обновление: приёмник запущен из папки, которую сейчас
			// заменят, — гасим и его, как на маке. Новая копия поднимет сама.
			if upd.Installing() {
				rcv.Stop()
			}
			os.Remove(filepath.Join(data, uiFile))
		})
	}
	defer shutdown()

	go func() {
		if err := autostart.Sync(a.Settings().StartWithWindows); err != nil {
			logLine("автозапуск: %v", err)
		}
	}()

	var dp atomic.Pointer[desktop.Desktop]
	var ending atomic.Bool // Windows выключается: обновление не ставим
	curLang := func() string { return i18n.Resolve(a.Settings().Language, i18n.System()) }
	srv, err := ui.Start(a, ui.Options{
		DataDir: data, LogPath: logPath, SessionFile: col.SessionFile(),
		OnSettings: func(old, cur settings.Settings) error {
			if d := dp.Load(); d != nil {
				d.Relabel()
			}
			if old.StartWithWindows == cur.StartWithWindows {
				return nil
			}
			if err := autostart.Sync(cur.StartWithWindows); err != nil {
				logLine("автозапуск: %v", err)
				cur.StartWithWindows = old.StartWithWindows
				a.SetSettings(cur)
				return err
			}
			logLine("автозапуск: %v", cur.StartWithWindows)
			return nil
		},
		OnShow: func() {
			if d := dp.Load(); d != nil {
				d.Show()
			}
		},
		OpenURL:    desktop.OpenURL,
		OpenFolder: desktop.OpenFolder,
		Version:    version,
		Update:     upd,
		// «Перезапустить сейчас»: сначала установка (проверка, права, запуск
		// установщика — он сам ждёт нашего выхода), и только если она пошла —
		// выходим. Не пошла — ничего не останавливаем, плашка скажет.
		OnUpdateRestart: func() error {
			if err := upd.Install(true); err != nil {
				return err
			}
			if d := dp.Load(); d != nil {
				go d.Quit()
			}
			return nil
		},
		SupportInfo: func() string {
			home, _ := os.UserHomeDir()
			return support.Info(version, support.OSVersion(), logPath, home)
		},
	})
	if err != nil {
		logLine("страница не запустилась: %v", err)
		desktop.Message("Albion Journal", i18n.Tf(lang, "msg.uiFailed", err))
		return
	}
	defer srv.Close()
	if b, err := json.Marshal(map[string]string{"url": srv.URL, "token": srv.Token}); err == nil {
		os.WriteFile(filepath.Join(data, uiFile), b, 0600)
	}
	logLine("страница: %s", srv.URL)

	d := desktop.New(desktop.Config{
		URL: srv.URL, Title: "Albion Journal", Hidden: *hidden, DataDir: data,
		Label:         func(k string) string { return i18n.T(curLang(), k) },
		Collecting:    a.Collecting,
		SetCollecting: a.SetCollecting,
		EndSession: func() {
			ending.Store(true)
			shutdown()
		},
		Logf: logLine,
	})
	dp.Store(d)

	packets := make(chan game.Packet, 4096)
	go sniffLoop(a, binDir, packets, col.Feed)
	go func() {
		for p := range packets {
			a.Feed(p)
		}
	}()
	go func() {
		last := a.Collecting()
		for now := range time.Tick(time.Second) {
			a.Tick(now)
			// Сбор переключили на странице — надпись в трее следом.
			if c := a.Collecting(); c != last {
				last = c
				d.Relabel()
			}
		}
	}()
	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		<-stop
		d.Quit()
	}()

	d.Run() // окно и трей; возвращается после «Выход»
	logLine("окно закрыто")
	// Выход с готовым обновлением и включённым автообновлением — ставим.
	// Установщик ждёт нашего выхода; своё гасит shutdown (и приёмник тоже,
	// раз установка пошла). Не пошла — выходим как обычно.
	if !ending.Load() && a.Settings().AutoUpdate && upd.Ready() != nil && !upd.Installing() {
		if err := upd.Install(false); err != nil {
			logLine("обновление при выходе не поставлено: %v", err)
		}
	}
}

// runApply — процесс установки обновления (запущен прежней копией из
// распакованной новой версии). Пишет в тот же журнал.
func runApply(p update.Plan) int {
	logw := io.Discard
	if p.DataDir != "" {
		if f, err := os.OpenFile(filepath.Join(p.DataDir, "albion-journal.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			defer f.Close()
			logw = f
		}
	}
	logf := func(format string, args ...any) {
		fmt.Fprintf(logw, "[обновление] %s %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
	}
	if p.Dest == "" || p.Prev == "" || p.DataDir == "" {
		logf("установка: не хватает -target, -prev или -data")
		return 2
	}
	logf("установщик %s: ставлю из %s в %s", version, p.Src, p.Dest)
	err := update.Apply(p, update.Env{
		WaitExit: update.WaitExit,
		// Только наш приёмник из этой папки (pid-файл + путь exe), чужой не трогаем.
		StopReceiver: func() { receiver.NewManager(filepath.Join(p.Dest, receiver.ExeName), p.DataDir, logw).Stop() },
		Unblock:      update.Unblock,
		Start:        func(exe string) error { return update.StartDetached(exe) },
		Logf:         logf,
	})
	if err != nil {
		return 1
	}
	return 0
}

// showOther просит уже запущенную копию показать окно. Не вышло —
// честное сообщение вместо молчаливого выхода.
func showOther(data, lang string) {
	if err := askShow(filepath.Join(data, uiFile), 10*time.Second); err != nil {
		desktop.Message("Albion Journal", i18n.Tf(lang, "msg.alreadyRunning", err))
	}
}

// askShow читает адрес страницы первой копии и просит показать окно.
// Первая копия могла только что запуститься и ещё не записать ui.json
// (UAC, запуск сборщика) — поэтому пробуем до wait.
func askShow(path string, wait time.Duration) error {
	c := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(wait)
	for {
		err := tryShow(c, path)
		if err == nil || time.Now().After(deadline) {
			return err
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func tryShow(c *http.Client, path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var u struct{ URL, Token string }
	if err := json.Unmarshal(b, &u); err != nil || u.URL == "" {
		return fmt.Errorf("%s: нет адреса страницы", filepath.Base(path))
	}
	req, err := http.NewRequest(http.MethodPost, u.URL+"api/show", bytes.NewReader(nil))
	if err != nil {
		return err
	}
	req.Header.Set(ui.TokenHeader, u.Token)
	r, err := c.Do(req)
	if err != nil {
		return err
	}
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return fmt.Errorf("окно не показано: %s", r.Status)
	}
	return nil
}

// sniffLoop держит драйвер открытым; если он упал, пробует снова через 5 секунд.
// raw получает пакеты для сборщика цен (тот же перехват, без второго драйвера).
func sniffLoop(a *app.App, binDir string, out chan<- game.Packet, raw func([]byte)) {
	for {
		d, err := sniff.Open(binDir)
		if err != nil {
			a.SetSniffError(err)
			time.Sleep(5 * time.Second)
			continue
		}
		a.SetSniffError(nil)
		err = d.Run(out, raw)
		d.Close()
		if err != nil {
			a.SetSniffError(err)
		}
		time.Sleep(time.Second)
	}
}

// runReplay прогоняет запись через ту же логику, время берётся из записи.
func runReplay(path string) int {
	f, err := os.Open(path)
	if err != nil {
		fmt.Println(err)
		return 1
	}
	defer f.Close()
	dir, _ := os.MkdirTemp("", "azf-replay")
	defer os.RemoveAll(dir)
	a := app.New(dir, dir, names.Zones())
	var last time.Time
	n := 0
	err = record.Read(f, func(p game.Packet) {
		n++
		if !last.IsZero() {
			for t := last.Add(time.Second); t.Before(p.T); t = t.Add(time.Second) {
				a.Tick(t)
			}
		}
		last = p.T
		a.Feed(p)
	})
	a.Tick(last.Add(time.Minute))
	if err != nil {
		fmt.Println("запись:", err)
	}
	st := a.State()
	fmt.Printf("пакетов %d, переходов %d\n", n, len(st.Recent))
	for i := len(st.Recent) - 1; i >= 0; i-- {
		t := st.Recent[i]
		res := "ок"
		if !t.OK {
			res = "ВЫЛЕТ: " + t.Fail
		}
		fmt.Printf("%s  %-28s → %-28s загрузка %5.1f с, ожило %5.1f с, %s, сервер %s\n",
			t.T.Format("15:04:05"), t.FromName, t.ToName, t.LoadSec, t.AliveSec, res, t.Server)
	}
	return 0
}
