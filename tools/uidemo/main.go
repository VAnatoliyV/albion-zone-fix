// uidemo поднимает окно программы на данных из записи — посмотреть интерфейс без Windows.
package main

import (
	"fmt"
	"os"
	"time"

	"albionzonefix/internal/app"
	"albionzonefix/internal/game"
	"albionzonefix/internal/names"
	"albionzonefix/internal/record"
	"albionzonefix/internal/ui"
)

func main() {
	dir, _ := os.MkdirTemp("", "azf-ui")
	a := app.New(dir, dir, names.Zones())
	f, _ := os.Open(os.Args[1])
	var last time.Time
	record.Read(f, func(p game.Packet) {
		for t := last.Add(time.Second); !last.IsZero() && t.Before(p.T); t = t.Add(time.Second) {
			a.Tick(t)
		}
		last = p.T
		a.Feed(p)
	})
	a.Tick(last.Add(time.Minute))
	a.SetSniffError(nil)
	url, _ := ui.Start(a)
	fmt.Println(url)
	select {}
}
