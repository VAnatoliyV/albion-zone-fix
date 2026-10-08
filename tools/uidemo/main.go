// uidemo — страница программы на поддельных данных: посмотреть все вкладки
// без Windows и без игры (режим разработки на маке).
//
//	go run ./tools/uidemo                 — пустая программа, как при первом запуске
//	go run ./tools/uidemo -fake           — свои цены, сессия, переходы
//	go run ./tools/uidemo -fake -receiver — то же и «приёмник работает, сайт готов»
//	go run ./tools/uidemo запись.azf      — переходы из настоящей записи
package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"albionzonefix/internal/app"
	"albionzonefix/internal/collector"
	"albionzonefix/internal/game"
	"albionzonefix/internal/names"
	"albionzonefix/internal/ownprices"
	"albionzonefix/internal/photon"
	"albionzonefix/internal/record"
	"albionzonefix/internal/settings"
	"albionzonefix/internal/ui"
)

// fakeCollector — разборщик, который только помнит настройки.
type fakeCollector struct{ cfg collector.Config }

func (f *fakeCollector) Apply(c collector.Config) error { f.cfg = c; return nil }
func (f *fakeCollector) ResetSession()                  {}
func (f *fakeCollector) Stats() collector.Stats {
	return collector.Stats{Running: f.cfg.Running(), Session: f.cfg.Running() && f.cfg.Session, Fed: 123456}
}

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "где слушать")
	fake := flag.Bool("fake", false, "подложить свои цены, сессию и переходы")
	recv := flag.Bool("receiver", false, "сделать вид, что приёмник работает и сайт готов")
	lang := flag.String("lang", "", "язык программы (ru, en, es)")
	flag.Parse()

	dir, _ := os.MkdirTemp("", "aj-ui")
	fmt.Println("данные:", dir)
	if *lang != "" {
		s := settings.Open(dir)
		v := s.Get()
		v.Language = *lang
		s.Set(v)
	}
	a := app.New(dir, dir, names.Zones())
	a.AttachCollector(&fakeCollector{})

	if *fake {
		writeFakes(dir)
		feedFakeTransitions(a)
	}
	if flag.NArg() > 0 {
		replay(a, flag.Arg(0))
	}
	a.SetSniffError(nil)

	logPath := filepath.Join(dir, "albion-journal.log")
	recvAddr := "127.0.0.1:1"
	if *recv {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err == nil {
			recvAddr = ln.Addr().String()
			go func() {
				for {
					c, err := ln.Accept()
					if err != nil {
						return
					}
					c.Close()
				}
			}()
		}
		os.WriteFile(logPath, []byte("запуск приёмника\nСайт готов\n"), 0644)
	}

	srv, err := ui.Start(a, ui.Options{
		Addr: *addr, DataDir: dir, LogPath: logPath, ReceiverAddr: recvAddr,
		SessionFile: filepath.Join(dir, collector.SessionFileName),
		OpenURL:     func(u string) { fmt.Println("открыть:", u) },
		OpenFolder:  func(d string) { fmt.Println("папка:", d) },
	})
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Println(srv.URL)
	select {}
}

func writeFakes(dir string) {
	now := time.Now().Unix()
	type row struct {
		Sell   int64 `json:"sell,omitempty"`
		SellTs int64 `json:"sellTs,omitempty"`
		Buy    int64 `json:"buy,omitempty"`
		BuyTs  int64 `json:"buyTs,omitempty"`
	}
	prices := map[string]row{
		"T4_BAG|Lymhurst|1":             {Sell: 2890, SellTs: now - 20, Buy: 2410, BuyTs: now - 20},
		"T5_2H_BOW|Lymhurst|2":          {Sell: 41200, SellTs: now - 25},
		"T6_MAIN_SWORD@1|Martlock|1":    {Sell: 151000, SellTs: now - 90, Buy: 120500, BuyTs: now - 95},
		"T4_ORE|Fort Sterling|1":        {Sell: 61, SellTs: now - 300, Buy: 55, BuyTs: now - 300},
		"T5_PLANKS|Bridgewatch|1":       {Sell: 412, SellTs: now - 4000},
		"T8_HEAD_PLATE_SET3|Caerleon|3": {Buy: 1820000, BuyTs: now - 7200},
		"T4_POTION_HEAL|Thetford|1":     {Sell: 255, SellTs: now - 86400*2},
	}
	b, _ := json.Marshal(map[string]any{"prices": prices, "built": now, "seenOrders": 18342})
	os.WriteFile(filepath.Join(dir, ownprices.FileName), b, 0644)

	session := map[string]any{
		"startedAt": now - 5400, "updatedAt": now - 40, "fame": 1843250, "fameEvents": 412,
		"silverEarned": 284310, "silverSpent": 12000, "silverBalance": 3120450, "silverLooted": 96400, "silverCity": 15200,
		"respec": 52000, "might": 1200, "favor": 300, "unknownHits": 17, "unknownSources": 2,
		"fighters": []map[string]any{
			{"name": "Krolik", "weapon": "T6_2H_BOW", "damage": 412300, "heal": 0, "firstAt": now - 600, "lastAt": now - 60},
			{"name": "Mayhem", "weapon": "T7_MAIN_SWORD@1", "damage": 298100, "heal": 12000, "firstAt": now - 620, "lastAt": now - 61},
			{"name": "HolyCow", "weapon": "T6_MAIN_HOLYSTAFF", "damage": 40100, "heal": 388000, "firstAt": now - 640, "lastAt": now - 62},
		},
	}
	b, _ = json.Marshal(session)
	os.WriteFile(filepath.Join(dir, collector.SessionFileName), b, 0644)
}

// pkt собирает пакет Photon (как в тестах app).
func pkt(msgType, opCode byte, payload []byte) []byte {
	hdr := []byte{0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0}
	data := append([]byte{0x00, msgType, opCode}, payload...)
	cmd := make([]byte, 12)
	cmd[0] = 6
	binary.BigEndian.PutUint32(cmd[4:], uint32(12+len(data)))
	return append(append(hdr, cmd...), data...)
}

func joinPkt(loc string) []byte {
	b := []byte{0, 0, photon.TypeNull, 2, 253, 11, game.OpJoin, 8, photon.TypeString, byte(len(loc))}
	return pkt(photon.MsgResponse, 1, append(b, loc...))
}

func feedFakeTransitions(a *app.App) {
	t := time.Now().Add(-20 * time.Minute)
	change := pkt(photon.MsgRequest, 1, []byte{1, 253, 11, game.OpChangeCluster})
	a.Feed(game.Packet{T: t, Addr: "193.169.238.17:5056", Payload: joinPkt("0007")})
	zones := []string{"1002", "4002", "3005", "0007", "2004"}
	for i, z := range zones {
		t = t.Add(2 * time.Minute)
		a.Feed(game.Packet{T: t, Out: true, Addr: "193.169.238.17:5056", Payload: change})
		srv := fmt.Sprintf("193.169.238.%d:5056", 100+i)
		a.Feed(game.Packet{T: t.Add(time.Duration(2+i) * time.Second), Addr: srv, Payload: joinPkt(z)})
		if i != 2 { // один переход — с чёрным экраном: сервер молчит
			a.Feed(game.Packet{T: t.Add(time.Duration(3+i) * time.Second), Addr: srv, Payload: pkt(photon.MsgEvent, 1, []byte{0})})
		}
		for s := t; s.Before(t.Add(90 * time.Second)); s = s.Add(time.Second) {
			a.Tick(s)
		}
	}
}

func replay(a *app.App, path string) {
	f, err := os.Open(path)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer f.Close()
	var last time.Time
	record.Read(f, func(p game.Packet) {
		for t := last.Add(time.Second); !last.IsZero() && t.Before(p.T); t = t.Add(time.Second) {
			a.Tick(t)
		}
		last = p.T
		a.Feed(p)
	})
	a.Tick(last.Add(time.Minute))
}
