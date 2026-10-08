// Пакет shared — общий снимок цен проекта (data.json сайта), как вкладка
// «Общие» мак-версии. Берём у своего приёмника, если он поднят (копия уже
// скачана и отдаётся мгновенно), иначе — с копии сайта на GitHub Pages.
package shared

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// PublicURL — общий снимок на GitHub Pages (открывается и из России).
const PublicURL = "https://vanatoliyv.github.io/albion-craft-profit/data.json"

// LocalURL — тот же снимок у своего приёмника.
const LocalURL = "http://localhost:7777/data.json"

// TTL — сколько держать прочитанный снимок: он весит ~5 МБ, а на сервере
// пересобирается раз в 15 минут.
const TTL = 5 * time.Minute

// Summary — что показывает вкладка.
type Summary struct {
	SnapshotTs  int64  `json:"snapshotTs"`
	Cities      int    `json:"cities"`
	Items       int    `json:"items"`
	BlackMarket int    `json:"blackMarket"`
	Resources   int    `json:"resources"`
	LoadedAt    int64  `json:"loadedAt"`
	Failed      bool   `json:"failed"`
	Error       string `json:"error,omitempty"`
	FromLocal   bool   `json:"fromLocal"`
}

// Parse считает сводку так же, как мак: предметы — объединение цен по
// городам (c) и цен Чёрного рынка (bo), иначе числа разойдутся с сайтом.
func Parse(r io.Reader) (Summary, error) {
	var root struct {
		T      float64                    `json:"t"`
		Cities []json.RawMessage          `json:"cities"`
		C      map[string]json.RawMessage `json:"c"`
		BO     map[string]json.RawMessage `json:"bo"`
		M      map[string]json.RawMessage `json:"m"`
	}
	if err := json.NewDecoder(r).Decode(&root); err != nil {
		return Summary{}, err
	}
	items := len(root.C)
	for k := range root.BO {
		if _, ok := root.C[k]; !ok {
			items++
		}
	}
	return Summary{SnapshotTs: int64(root.T), Cities: len(root.Cities), Items: items,
		BlackMarket: len(root.BO), Resources: len(root.M)}, nil
}

// Loader читает снимок и держит его TTL.
type Loader struct {
	Public, Local string
	LocalUp       func() bool // поднят ли приёмник
	Client        *http.Client

	mu   sync.Mutex
	last Summary
	busy bool
}

// New — загрузчик с адресами по умолчанию.
func New(localUp func() bool) *Loader {
	return &Loader{Public: PublicURL, Local: LocalURL, LocalUp: localUp, Client: &http.Client{Timeout: 60 * time.Second}}
}

// Get отдаёт сводку; устарела или force — перечитывает (один запрос за раз:
// пока идёт чтение, остальные получают прошлую сводку).
func (l *Loader) Get(ctx context.Context, force bool) Summary {
	l.mu.Lock()
	fresh := l.last.LoadedAt > 0 && time.Since(time.Unix(l.last.LoadedAt, 0)) < TTL
	if (fresh && !force) || l.busy {
		s := l.last
		l.mu.Unlock()
		return s
	}
	l.busy = true
	l.mu.Unlock()

	s := l.load(ctx)

	l.mu.Lock()
	l.busy = false
	// Неудача не затирает хороший снимок: показываем старый, а не ошибку.
	if !s.Failed || l.last.LoadedAt == 0 || l.last.Failed {
		l.last = s
	}
	out := l.last
	l.mu.Unlock()
	return out
}

func (l *Loader) load(ctx context.Context) Summary {
	if l.LocalUp != nil && l.LocalUp() {
		if s, err := l.fetch(ctx, l.Local); err == nil {
			s.FromLocal = true
			return s
		}
	}
	s, err := l.fetch(ctx, l.Public)
	if err != nil {
		return Summary{Failed: true, Error: err.Error(), LoadedAt: time.Now().Unix()}
	}
	return s
}

func (l *Loader) fetch(ctx context.Context, url string) (Summary, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Summary{}, err
	}
	resp, err := l.Client.Do(req)
	if err != nil {
		return Summary{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Summary{}, fmt.Errorf("%s: %s", url, resp.Status)
	}
	s, err := Parse(resp.Body)
	if err != nil {
		return Summary{}, err
	}
	s.LoadedAt = time.Now().Unix()
	return s, nil
}
