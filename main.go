// Albion Journal для Windows — сбор цен, счётчик фейма и урона и Zone Fix
// (статистика переходов между локациями Albion Online и обход чёрного экрана
// для игроков, у которых провайдер портит UDP игры).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"albionzonefix/internal/app"
	"albionzonefix/internal/autostart"
	"albionzonefix/internal/avalon"
	"albionzonefix/internal/collector"
	"albionzonefix/internal/datadir"
	"albionzonefix/internal/desktop"
	"albionzonefix/internal/game"
	"albionzonefix/internal/gameguard"
	"albionzonefix/internal/gamewatch"
	"albionzonefix/internal/hotkey"
	"albionzonefix/internal/i18n"
	"albionzonefix/internal/names"
	"albionzonefix/internal/notify"
	"albionzonefix/internal/ocr"
	"albionzonefix/internal/oldcopy"
	"albionzonefix/internal/overlay"
	"albionzonefix/internal/pwsh"
	"albionzonefix/internal/receiver"
	"albionzonefix/internal/record"
	"albionzonefix/internal/screen"
	"albionzonefix/internal/settings"
	"albionzonefix/internal/sniff"
	"albionzonefix/internal/support"
	"albionzonefix/internal/ui"
	"albionzonefix/internal/update"
	"albionzonefix/internal/zonecard"
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
	findOld := flag.String("find-old", "", "для установщика: число старых копий вне этой папки установки (код выхода)")
	removeOld := flag.String("remove-old", "", "для установщика: закрыть и удалить старые копии вне этой папки установки")
	watchGame := flag.Bool(gameguard.Flag, false, "сторож игры: поднять программу, когда запустится Albion (без окна)")
	fromWatch := flag.Bool(gameguard.FromFlag, false, "программу поднял сторож игры: игра только что запустилась")
	stopGuard := flag.Bool(gameguard.StopFlag, false, "для установщика: закрыть сторожа игры (код 0 — закрыт, 1 — не было)")
	flag.Parse()

	// Версия — до всего остального (без прав администратора, без окна и без
	// проверки второй копии): для тестера и поддержки.
	if *showVersion {
		fmt.Println(version)
		return
	}
	if *apply != "" {
		os.Exit(runApply(update.Plan{Src: *apply, Dest: *applyTarget, Prev: *applyPrev,
			PID: *applyPID, Restart: *applyRestart, DataDir: *applyData}))
	}
	// Старые копии в других папках (распакованный zip) — для установщика,
	// тоже до проверки второй копии: старая копия как раз и запущена.
	if *findOld != "" {
		os.Exit(runFindOld(*findOld))
	}
	if *removeOld != "" {
		os.Exit(runRemoveOld(*removeOld))
	}
	if *stopGuard {
		os.Exit(runStopWatch())
	}

	exe, _ := os.Executable()
	// Сторож игры — до проверки второй копии и прав: у него свой знак одной
	// копии, а программу он поднимает сам (internal/gameguard).
	if *watchGame {
		os.Exit(runWatch(exe))
	}
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
		// Поднял сторож, а программа уже есть (успела запуститься сама) —
		// она и следит за игрой; окно поверх игры не выталкиваем.
		if *fromWatch {
			return
		}
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
	// Сторож игры больше не нужен: программа следит сама. Закрыть до
	// всего остального, чтобы он не поднял вторую копию.
	if was, ok := gameguard.StopRunning(3 * time.Second); was {
		logLine("сторож игры закрыт (%v)", ok)
	}
	// Подняла задача Планировщика по просьбе сторожа без прав — тоже «игра
	// только что запустилась».
	if !*fromWatch && gameguard.TakeMarker(data, time.Now()) {
		*fromWatch = true
	}
	if *fromWatch {
		logLine("подняла сторож игры: игра только что запустилась")
	}

	binDir := filepath.Join(dir, "zapret", "bin")
	if moved, err := datadir.Migrate(dir, data); len(moved) > 0 || err != nil {
		logLine("перенёс файлы Zone Fix в %s: %v %v", data, moved, err)
	}
	a := app.New(data, binDir, names.Zones())
	a.SetLog(logLine)

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
	// Скачивание и распаковка — в %ProgramData%\Albion Journal\update (только
	// SYSTEM и администраторы), не в %AppData%: оттуда файлы с правами
	// администратора идут в папку программы.
	stage, err := update.StageDir()
	if err != nil {
		logLine("обновление: нет папки ProgramData: %v", err)
	}
	upd := update.New(update.Config{
		Dir: stage, Current: version, ProgramDir: dir, DataDir: data,
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
		if err := autostart.Sync(gameguard.TaskMode(a.Settings())); err != nil {
			logLine("автозапуск: %v", err)
		}
	}()

	var dp atomic.Pointer[desktop.Desktop]

	// Карта Авалона: проходы по дорогам — на общий сервер карты (только
	// Европа), смена зоны — подсветка на открытом окне карты.
	mapRep := avalon.NewReporter(avalon.ReporterConfig{
		Install: a.MapInstall,
		Logf: func(format string, args ...any) {
			fmt.Fprintf(logw, "[карта] %s %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
		},
	})
	a.AttachMap(mapRep, func(code string) {
		if d := dp.Load(); d != nil {
			d.MapEval(avalon.HereJS(code))
		}
	})

	var ending atomic.Bool // Windows выключается: обновление не ставим
	curLang := func() string { return i18n.Resolve(a.Settings().Language, i18n.System()) }

	// Карточка зоны по кнопке (этап 4): хук кнопки → снимок у курсора →
	// распознавание (своё, запасное — OCR Windows) → опознание → вкладка «Зона», уведомление или панель
	// поверх игры и отчёт портала на карту. Панель — только если человек
	// сам выбрал её в настройках (по умолчанию уведомление).
	cardLog := func(format string, args ...any) {
		fmt.Fprintf(logw, "[карточка] %s %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
	}
	notify.Init(data, desktop.Icon, cardLog)
	// Рабочий PowerShell для OCR и уведомлений — заранее, чтобы первое
	// нажатие не ждало запуска (карточка или уведомления включены).
	pwsh.SetLog(cardLog)
	if set := a.Settings(); hotkey.Normalize(set.ZoneKey) != hotkey.Off || set.ZoneShow == settings.ShowNotify || set.BlackWarn {
		pwsh.Warm(cardLog)
	}
	defer pwsh.Stop()
	// Панель поверх игры: своё окно на своём потоке, создаётся при первом показе.
	panel := overlay.New(cardLog)
	defer panel.Close()
	dict := zonecard.Default()
	if dict == nil {
		cardLog("справочник зон не прочитался")
	}
	var mapHint zonecard.HintGate
	// Своё распознавание (PaddleOCR через ONNX Runtime, папка ocr рядом с
	// программой) — первым; Windows OCR — запасной. Модель грузится фоном
	// при запуске, если карточка включена, иначе при первом нажатии.
	textOCR := &ocr.Combined{
		Dir:     filepath.Join(dir, "ocr"),
		Threads: 2,
		Native:  screen.Native,
		Windows: ocr.Recognize,
		Logf:    cardLog,
	}
	runner := zonecard.NewRunner(zonecard.RunnerConfig{
		Path: filepath.Join(data, screen.FileName),
		Capture: func(path string) (zonecard.Snap, error) {
			info, err := screen.CaptureAroundCursor(path)
			text := fmt.Sprintf("курсор %v, dpi %d, монитор %v, рамка %v, для OCR %v (×%d)", info.Cursor, info.DPI, info.Mon, info.Rect, info.Out, info.Scale)
			if info.Tries > 1 {
				text += fmt.Sprintf(", пиксели со %d-го раза", info.Tries)
			}
			if !info.Crop.Empty() {
				text += fmt.Sprintf(", обрезано по тултипу %dx%d", info.Crop.Dx(), info.Crop.Dy())
			}
			return zonecard.Snap{Info: text, Empty: info.Empty, Cropped: !info.Crop.Empty()}, err
		},
		// Повторы и варианты картинки (серый, инверсия) — тултип мог не
		// дорисоваться, текст мог прочитаться плохо.
		Retry:     true,
		Prepare:   screen.Variant,
		Variants:  func() bool { return !a.Settings().ZoneOCRPlain },
		Recognize: textOCR.Recognize,
		Live:      pwsh.Live,
		Languages: ocr.Languages,
		Pick:      textOCR.Pick,
		Hint:      textOCR.Hint,
		Dict:      dict,
		Gap:       400 * time.Millisecond,
		Logf:      cardLog,
		Done: func(sh zonecard.Shot) {
			cardLog("карта: %s", a.SetCard(sh, dict))
			if zonecard.Doubtful(sh) {
				// Сомнительное опознание — снимок отдельно, чтобы тестер прислал его
				// (цветной снимок, по которому вышел итог: при повторах — не
				// обязательно первый; вариант — серый/инверсия — рядом в папке).
				src := filepath.Join(data, screen.FileName)
				if sh.Source != "" {
					src = sh.Source
				}
				if b, err := os.ReadFile(src); err == nil {
					if err := os.WriteFile(filepath.Join(data, zonecard.DoubtFile), b, 0644); err != nil {
						cardLog("снимок сомнительной карточки не сохранён: %v", err)
					} else {
						cardLog("снимок сомнительной карточки: %s", zonecard.DoubtFile)
					}
				}
			}
			// Кроме вкладки — уведомлением или панелью поверх игры, по
			// настройке «Способ показа», и только включённые части.
			set, lang, now := a.Settings(), curLang(), time.Now()
			toast := func(t *zonecard.Toast) {
				if t != nil {
					notify.Show(t.Title, t.Subtitle, t.Body)
				}
			}
			// Сомнительно или не узнано — подсказка «точнее на карте мира (M)»,
			// не чаще раза в zonecard.MapHintEvery.
			hint := zonecard.WantsMapHint(sh) && mapHint.Allow(now)
			// Панель упала в этом запуске — сразу уведомлением.
			pr := zonecard.PresentWith(lang, sh, set, now, panel.OK(), hint)
			toast(pr.Toast)
			if pr.Panel != nil {
				fallback := func() { toast(zonecard.PresentWith(lang, sh, set, now, false, hint).Toast) }
				if !panel.Show(*pr.Panel, set.ZoneOverlayCorner, set.ZoneOverlaySec, fallback) {
					fallback()
				}
			}
		},
	})
	a.AttachCard(func() (bool, []string, string, bool) {
		langs, hint, checked := runner.OCRStatus()
		return runner.Busy(), langs, hint, checked
	})
	go runner.CheckLanguages(context.Background())
	// Своё распознавание не загрузилось или сломалось — языки Windows OCR
	// снова важны: проверить заново, чтобы подсказка «установите язык»
	// была видна.
	textOCR.Unavailable = func() { go runner.CheckLanguages(context.Background()) }
	if hotkey.Normalize(a.Settings().ZoneKey) != hotkey.Off {
		go textOCR.Warm()
	}
	var (
		hookMu  sync.Mutex
		hook    *hotkey.Hook
		hookKey hotkey.Key
	)
	setKey := func(k hotkey.Key) {
		hookMu.Lock()
		defer hookMu.Unlock()
		if k == hookKey && hook != nil {
			return
		}
		hook.Stop()
		hook, hookKey = nil, k
		h, err := hotkey.Start(k, func() { panel.Mark(); runner.Trigger() }, cardLog)
		if err != nil {
			cardLog("кнопка %s не поставлена: %v", k, err)
			return
		}
		hook = h
	}
	setKey(hotkey.Normalize(a.Settings().ZoneKey))
	defer func() {
		hookMu.Lock()
		hook.Stop()
		hookMu.Unlock()
	}()

	// Предупреждение о чёрном экране: новый сервер молчит после CONNECT.
	a.OnStall(func(server, from string) {
		lang := curLang()
		body := i18n.T(lang, "msg.blackBody")
		if from != "" {
			body = i18n.Tf(lang, "msg.blackFrom", from)
		}
		cardLog("чёрный экран? сервер %s молчит после подключения (из %q)", server, from)
		notify.Show(i18n.T(lang, "msg.blackTitle"), "", body)
	})
	srv, err := ui.Start(a, ui.Options{
		DataDir: data, LogPath: logPath, SessionFile: col.SessionFile(),
		OnSettings: func(old, cur settings.Settings) error {
			if d := dp.Load(); d != nil {
				d.Relabel()
			}
			setKey(hotkey.Normalize(a.Settings().ZoneKey))
			// Задача при входе: программа, сторож игры или ничего.
			mode := gameguard.TaskMode(cur)
			rollback, err := gameguard.SyncTask(old, cur, autostart.Sync)
			if err != nil {
				logLine("автозапуск (%v): %v", mode, err)
				// Откатываем только «Запускать вместе с Windows»: без задачи
				// сторожа «вместе с игрой» работает, пока программа запущена,
				// и после выхода (сторожа поднимает она сама) — но не после
				// перезагрузки, это и скажет страница.
				if rollback {
					cur.StartWithWindows = old.StartWithWindows
					a.SetSettings(cur)
					return err
				}
				return fmt.Errorf("%s: %w", i18n.T(curLang(), "msg.watchTaskFailed"), err)
			}
			if gameguard.TaskMode(old) != mode {
				logLine("автозапуск: %v", mode)
			}
			return nil
		},
		RecordKey: func(ctx context.Context, timeout time.Duration, hint func(string)) (string, error) {
			k, err := hotkey.Record(ctx, timeout, hint, cardLog)
			return string(k), err
		},
		OnShow: func() {
			if d := dp.Load(); d != nil {
				d.Show()
			}
		},
		OpenURL:    desktop.OpenURL,
		OpenFolder: desktop.OpenFolder,
		OpenMap: func() {
			u := avalon.MapURL(a.HereCode())
			if d := dp.Load(); d != nil {
				d.OpenMap(u)
			} else {
				desktop.OpenURL(u)
			}
		},
		Version: version,
		Update:  upd,
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
		WindowHidden: func() bool {
			if d := dp.Load(); d != nil {
				return d.Hidden()
			}
			return *hidden
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
		WatchStays: func() bool { return gameguard.Wanted(a.Settings()) },
		Logf:       logLine,
	})
	dp.Store(d)

	// Вместе с игрой (как СторожИгры у мака): снимок процессов раз в 3 с,
	// пока включена хоть одна из трёх настроек. Программа может подняться
	// с Windows раньше игры — сторож ждёт её появления.
	gameOpts := func(s settings.Settings) gamewatch.Options {
		return gamewatch.Options{Show: s.ShowWithGame, Collect: s.StartWithGame, Quit: s.QuitWithGame}
	}
	watchCtx, stopWatch := context.WithCancel(context.Background())
	defer stopWatch()
	go func() {
		// Следить — когда окно уже есть: показывать и закрывать до него нечего.
		select {
		case <-d.Ready():
		case <-watchCtx.Done():
			return
		}
		gamewatch.Run(watchCtx, gamewatch.Config{
			JustStarted: *fromWatch,
			Options:     func() gamewatch.Options { return gameOpts(a.Settings()) },
			On: func(e gamewatch.Event) {
				act := gamewatch.Decide(e, gameOpts(a.Settings()), a.Collecting())
				logLine("сторож игры: %v (показать %v, сбор %v, выход %v)", e, act.Show, act.Collect, act.Quit)
				if act.Show {
					// Один раз на запуск игры и после того, как появится её окно.
					time.AfterFunc(gamewatch.ShowDelay, func() {
						if watchCtx.Err() == nil && a.Settings().ShowWithGame {
							d.ShowFront()
						}
					})
				}
				if act.Collect {
					if err := a.SetCollecting(true); err != nil {
						logLine("сбор вместе с игрой не запустился: %v", err)
					}
				}
				if act.Quit {
					d.Quit()
				}
			},
		})
	}()

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
		// «Сервер зоны не отвечает» — через ~2 с, а не через 2–3 (Tick раз в секунду).
		for now := range time.Tick(250 * time.Millisecond) {
			a.CheckStall(now)
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
	// «Вместе с игрой» работает и у закрытой программы: остаётся сторож.
	// Ставится обновление — его поднимет установщик (сторож — тот же exe).
	if d.QuitAll() && gameguard.Wanted(a.Settings()) {
		logLine("выход совсем: сторож игры не остаётся")
	}
	if gameguard.OnExit(a.Settings(), ending.Load(), upd.Installing(), d.QuitAll()) {
		if err := update.StartDetached(exe, "-"+gameguard.Flag); err != nil {
			logLine("сторож игры не запущен: %v", err)
		} else {
			logLine("остаётся сторож игры")
		}
	}
}

// fileLog — журнал программы для служебных режимов без окна (установщик,
// сторож игры). Закрыть — второе значение.
func fileLog(data, prefix string) (func(string, ...any), func()) {
	if data == "" {
		return func(string, ...any) {}, func() {}
	}
	f, err := os.OpenFile(filepath.Join(data, "albion-journal.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return func(string, ...any) {}, func() {}
	}
	return func(format string, args ...any) {
		fmt.Fprintf(f, "%s %s %s\n", prefix, time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
	}, func() { f.Close() }
}

// savedSettings — настройки из файла, без записи умолчаний (служебным
// режимам создавать файл настроек незачем). ok=false — файла нет.
func savedSettings(data string) (settings.Settings, bool) {
	if _, err := os.Stat(filepath.Join(data, settings.FileName)); err != nil {
		return settings.Settings{}, false
	}
	return settings.Open(data).Get(), true
}

// runWatch — сторож игры (-watch-game): ждёт запуска Albion, поднимает
// программу и выходит. Без окна, WebView2, WinDivert и PowerShell.
func runWatch(exe string) int {
	debug.SetGCPercent(25) // памяти — минимум: куча у сторожа крошечная
	data, err := datadir.Dir()
	if err != nil {
		data = filepath.Dir(exe)
	}
	logf, done := fileLog(data, "[сторож]")
	defer done()
	admin := isAdmin()
	logf("сторож игры %s: запуск (администратор %v)", version, admin)
	r := gameguard.Serve(gameguard.Env{
		Acquire: gameguard.Acquire,
		Wanted: func() bool {
			s, ok := savedSettings(data)
			return ok && gameguard.Wanted(s)
		},
		Running:    gamewatch.Running,
		AppRunning: desktop.OtherRunning,
		Launch: func() error {
			task := autostart.Off
			if !admin {
				_, task, _ = autostart.Current()
			}
			way := gameguard.HowToLaunch(admin, task)
			logf("поднимаю программу %v", way)
			if way == gameguard.ViaTask {
				return gameguard.LaunchViaTask(data, time.Now(), autostart.Run)
			}
			// Ask: программа без прав сама попросит их (окно UAC) — иначе
			// никак: задача не запускает программу (или её нет).
			return update.StartDetached(exe, gameguard.LaunchArgs()...)
		},
		Logf: logf,
	})
	logf("сторож игры: выход — %v", r)
	if r == gameguard.Failed {
		return 1
	}
	return 0
}

// runStopWatch — закрыть сторожа игры (-stop-watch, для установщика).
func runStopWatch() int {
	data, _ := datadir.Dir()
	logf, done := fileLog(data, "[сторож]")
	defer done()
	was, ok := gameguard.StopRunning(5 * time.Second)
	switch {
	case !was:
		return 1
	case !ok:
		logf("установщик: сторож игры не закрылся")
		return 2
	}
	logf("установщик: сторож игры закрыт")
	return 0
}

// oldLog — журнал программы для -find-old/-remove-old (пишет установщик,
// своего окна нет). Закрыть — второе значение.
func oldLog() (func(string, ...any), func()) {
	data, err := datadir.Dir()
	if err != nil {
		return func(string, ...any) {}, func() {}
	}
	return fileLog(data, "[старая копия]")
}

// runFindOld: код выхода — 100 + число найденных старых копий (100 — нет).
// Остальные коды (паника Go — 2, ошибка флагов, сбой) установщик считает
// ошибкой и молча пропускает шаг.
func runFindOld(inst string) int {
	logf, done := oldLog()
	defer done()
	n := len(oldcopy.Find(inst, oldcopy.System(logf)))
	logf("установщик (%s): старых копий %d", inst, n)
	return findOldCode(n)
}

func findOldCode(n int) int { return 100 + min(n, 99) }

// runRemoveOld закрывает и удаляет старые копии; 0 — все убраны.
func runRemoveOld(inst string) int {
	logf, done := oldLog()
	defer done()
	if failed := oldcopy.Remove(inst, oldcopy.System(logf), oldcopy.WinCloser{}, oldcopy.Retarget); failed > 0 {
		return 1
	}
	return 0
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
		StopWatch: func() {
			if was, ok := gameguard.StopRunning(5 * time.Second); was {
				logf("сторож игры закрыт перед установкой (%v)", ok)
			}
		},
		Unblock: update.Unblock,
		Start:   func(exe string) error { return update.StartDetached(exe) },
		Logf:    logf,
	})
	// Без перезапуска программы (обновление при выходе) — вернуть сторожа
	// игры, которого программа ради установки не оставила.
	if !p.Restart {
		if s, ok := savedSettings(p.DataDir); ok && gameguard.Wanted(s) {
			if serr := update.StartDetached(filepath.Join(p.Dest, update.MainExe), "-"+gameguard.Flag); serr != nil {
				logf("сторож игры не запущен: %v", serr)
			} else {
				logf("сторож игры снова на месте")
			}
		}
	}
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
