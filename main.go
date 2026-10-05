// Albion Zone Fix — статистика переходов между локациями Albion Online
// и обход чёрного экрана для игроков, у которых провайдер портит UDP игры.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"albionzonefix/internal/app"
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
			fmt.Println("Нажмите на AlbionZoneFix.exe правой кнопкой → «Запуск от имени администратора».")
			waitEnter()
		}
		return
	}

	binDir := filepath.Join(dir, "zapret", "bin")
	a := app.New(dir, binDir, names.Zones())
	defer a.Shutdown()

	url, err := ui.Start(a)
	if err != nil {
		fmt.Println("Окно не запустилось:", err)
		waitEnter()
		return
	}
	fmt.Println("Albion Zone Fix работает. Окно:", url)
	fmt.Println("Чтобы выйти, закройте это окно консоли. Обход выключится сам.")
	if !*noBrowser {
		openBrowser(url)
	}

	packets := make(chan game.Packet, 4096)
	go sniffLoop(a, binDir, packets)
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
func sniffLoop(a *app.App, binDir string, out chan<- game.Packet) {
	for {
		d, err := sniff.Open(binDir)
		if err != nil {
			a.SetSniffError(err)
			time.Sleep(5 * time.Second)
			continue
		}
		a.SetSniffError(nil)
		err = d.Run(out)
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
