// Albion Journal для Windows — сбор цен, счётчик фейма и урона и Zone Fix
// (статистика переходов между локациями Albion Online и обход чёрного экрана
// для игроков, у которых провайдер портит UDP игры).
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"albionzonefix/internal/app"
	"albionzonefix/internal/collector"
	"albionzonefix/internal/datadir"
	"albionzonefix/internal/game"
	"albionzonefix/internal/names"
	"albionzonefix/internal/record"
	"albionzonefix/internal/sniff"
	"albionzonefix/internal/ui"
)

func main() {
	replay := flag.String("replay", "", "прогнать запись .azf и напечатать переходы")
	noBrowser := flag.Bool("no-browser", false, "не открывать окно в браузере")
	flag.Parse()

	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	if *replay != "" {
		os.Exit(runReplay(*replay))
	}

	if !isAdmin() {
		fmt.Println("Нужны права администратора: драйверу перехвата пакетов без них нельзя. Перезапускаю…")
		if err := relaunchAsAdmin(); err != nil {
			fmt.Println("Не получилось:", err)
			fmt.Println("Нажмите на AlbionJournal.exe правой кнопкой → «Запуск от имени администратора».")
			waitEnter()
		}
		return
	}

	binDir := filepath.Join(dir, "zapret", "bin")
	data, err := datadir.Dir()
	if err != nil {
		fmt.Println("Нет каталога данных, пишу рядом с программой:", err)
		data = dir
	}
	if moved, err := datadir.Migrate(dir, data); len(moved) > 0 || err != nil {
		fmt.Println("Перенёс файлы Zone Fix в", data+":", moved, err)
	}
	a := app.New(data, binDir, names.Zones())
	defer a.Shutdown()

	// Разборщик сборщика цен: журнал — в каталог данных, таблица предметов — рядом с exe.
	logf, err := os.OpenFile(filepath.Join(data, "albion-journal.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println("Журнал не открылся:", err)
		logf = nil
	}
	col := collector.New(data, dir, logWriter(logf))
	if err := a.AttachCollector(col); err != nil {
		fmt.Println("Сборщик цен не запустился:", err)
	}

	url, err := ui.Start(a)
	if err != nil {
		fmt.Println("Окно не запустилось:", err)
		waitEnter()
		return
	}
	fmt.Println("Albion Journal работает. Окно:", url)
	fmt.Println("Данные:", data)
	fmt.Println("Чтобы выйти, закройте это окно консоли. Обход выключится сам.")
	if !*noBrowser {
		openBrowser(url)
	}

	packets := make(chan game.Packet, 4096)
	go sniffLoop(a, binDir, packets, col.Feed)
	go func() {
		for p := range packets {
			a.Feed(p)
		}
	}()
	go func() {
		for now := range time.Tick(time.Second) {
			a.Tick(now)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	fmt.Println("Выключаю обход и выхожу…")
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

// logWriter — nil-файл превращает в «никуда», чтобы клиент не писал в консоль.
func logWriter(f *os.File) io.Writer {
	if f == nil {
		return io.Discard
	}
	return f
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
