// Пакет ownprices читает базу своих цен приёмника (acp-own-prices.json в
// каталоге данных) — то же, что вкладка «Свои» мак-версии (Reader.snapshot).
package ownprices

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
)

// FileName — база приёмника в каталоге данных (тот же формат, что у мака).
const FileName = "acp-own-prices.json"

// RecentLimit — сколько последних позиций показывать.
const RecentLimit = 50

type row struct {
	Sell   *int64  `json:"sell"`
	Buy    *int64  `json:"buy"`
	SellTs float64 `json:"sellTs"`
	BuyTs  float64 `json:"buyTs"`
}

type db struct {
	Prices     map[string]row `json:"prices"`
	Built      float64        `json:"built"`
	SeenOrders int64          `json:"seenOrders"`
}

// Item — одна позиция: предмет, город, качество и цены.
type Item struct {
	ID      string `json:"id"` // ключ базы: предмет|город|качество
	Name    string `json:"name"`
	City    string `json:"city"`
	Quality string `json:"quality"`
	Sell    *int64 `json:"sell,omitempty"` // почём купить (самый дешёвый селл-ордер)
	Buy     *int64 `json:"buy,omitempty"`  // почём сдать (самый дорогой бай-ордер)
	Ts      int64  `json:"ts"`
}

// Summary — то, что показывает вкладка.
type Summary struct {
	Exists    bool   `json:"exists"`
	Positions int    `json:"positions"`
	Orders    int64  `json:"orders"`
	Cities    int    `json:"cities"`
	LastTs    int64  `json:"lastTs"`
	Recent    []Item `json:"recent"`
	Error     string `json:"error,omitempty"`
}

// Read читает базу. Нет файла — Exists=false и без ошибки: приёмник ещё не
// запускался. Битый файл (приёмник пишет его через временный, но мало ли) —
// ошибка в Summary.Error.
func Read(path string) Summary {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Summary{}
		}
		return Summary{Error: err.Error()}
	}
	var d db
	if err := json.Unmarshal(b, &d); err != nil {
		return Summary{Error: err.Error()}
	}
	s := Summary{Exists: true, Positions: len(d.Prices), Orders: d.SeenOrders}
	cities := map[string]bool{}
	items := make([]Item, 0, len(d.Prices))
	for k, r := range d.Prices {
		parts := strings.Split(k, "|")
		it := Item{ID: k, Name: parts[0], City: "—", Sell: r.Sell, Buy: r.Buy}
		if len(parts) > 1 {
			it.City = parts[1]
		}
		if len(parts) > 2 {
			it.Quality = parts[2]
		}
		ts := r.SellTs
		if r.BuyTs > ts {
			ts = r.BuyTs
		}
		it.Ts = int64(ts)
		cities[it.City] = true
		if it.Ts > s.LastTs {
			s.LastTs = it.Ts
		}
		items = append(items, it)
	}
	s.Cities = len(cities)
	// С довеском по ключу: у позиций с одной отметкой времени (а с одного
	// аукциона их сразу несколько) порядок иначе прыгал бы при каждом опросе.
	sort.Slice(items, func(i, j int) bool {
		if items[i].Ts != items[j].Ts {
			return items[i].Ts > items[j].Ts
		}
		return items[i].ID < items[j].ID
	})
	if len(items) > RecentLimit {
		items = items[:RecentLimit]
	}
	s.Recent = items
	return s
}
