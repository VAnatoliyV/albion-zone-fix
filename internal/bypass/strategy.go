// Пакет bypass запускает движок zapret (winws.exe) только для трафика Albion
// и подбирает стратегию, при которой переходы между локациями проходят лучше.
package bypass

import (
	"encoding/json"
	"os"
	"strings"

	"albionzonefix/internal/zones"
)

// Порты Albion: 5055 — мастер-сервер, 5056 — игровые серверы (Photon).
const albionPorts = "5055,5056"

// Strategy — параметры winws. {BIN} заменяется на папку с winws.exe.
type Strategy struct {
	Name string   `json:"name"`
	Args []string `json:"args"`
}

// DefaultStrategies — UDP-часть «игрового фильтра» из сборки Flowseal 1.10.3
// (general*.bat), без ipset: нам нужны только порты Albion.
func DefaultStrategies() []Strategy {
	fake := func(repeats, cutoff string, payloads ...string) []string {
		a := []string{"--dpi-desync=fake", "--dpi-desync-repeats=" + repeats, "--dpi-desync-any-protocol=1"}
		for _, p := range payloads {
			a = append(a, "--dpi-desync-fake-unknown-udp={BIN}"+p)
		}
		return append(a, "--dpi-desync-cutoff="+cutoff)
	}
	g := "ACTIVE_GAME_UDP.bin"
	return []Strategy{
		{"Стандарт (12×, n2)", fake("12", "n2", g)},
		{"Стандарт n3 (12×, n3)", fake("12", "n3", g)},
		{"Мягкая (10×, n2)", fake("10", "n2", g)},
		{"Мягкая n4 (10×, n4)", fake("10", "n4", g)},
		{"Усиленная (14×, n3)", fake("14", "n3", g)},
		{"Экспериментальная (5×, n4, QUIC)", fake("5", "n4", "quic_initial_4pda_to.bin", g)},
		{"Короткая (6×, n2)", fake("6", "n2", g)},
	}
}

// LoadStrategies читает strategies.json рядом с программой, если он есть,
// иначе отдаёт стратегии по умолчанию (и записывает их туда для правки).
func LoadStrategies(path string) []Strategy {
	if b, err := os.ReadFile(path); err == nil {
		var s []Strategy
		if json.Unmarshal(b, &s) == nil && len(s) > 0 {
			return s
		}
	}
	d := DefaultStrategies()
	if b, err := json.MarshalIndent(d, "", "  "); err == nil {
		os.WriteFile(path, b, 0644)
	}
	return d
}

// Args — полная командная строка winws для стратегии.
func Args(s Strategy, binDir string) []string {
	a := []string{"--wf-udp=" + albionPorts, "--filter-udp=" + albionPorts}
	for _, x := range s.Args {
		a = append(a, strings.ReplaceAll(x, "{BIN}", binDir))
	}
	return a
}

// Picker — режим «подобрать»: каждая стратегия получает perStrategy переходов,
// оценка = вылеты×10 + среднее (загрузка + ожидание первого события).
type Picker struct {
	list   []Strategy
	per    int
	idx    int
	n      int
	scores []float64
}

func NewPicker(list []Strategy, perStrategy int) *Picker {
	return &Picker{list: list, per: perStrategy, scores: make([]float64, len(list))}
}

func (p *Picker) Start() Strategy { p.idx, p.n = 0, 0; return p.list[0] }

func (p *Picker) Current() Strategy { return p.list[p.idx] }

func (p *Picker) Progress() (strategy, of, transition, per int) {
	return p.idx + 1, len(p.list), p.n, p.per
}

// OnTransition учитывает переход. Возвращает следующую стратегию, когда
// текущая отработала, и done=true, когда перебраны все.
func (p *Picker) OnTransition(tr zones.Transition) (next *Strategy, done bool) {
	if p.idx >= len(p.list) || tr.Strategy != p.list[p.idx].Name {
		return nil, false
	}
	if !tr.OK {
		p.scores[p.idx] += 10 * float64(p.per) // делим на per ниже: вылет = +10 к среднему
	} else {
		alive := tr.AliveSec
		if alive < 0 {
			alive = 0
		}
		p.scores[p.idx] += tr.LoadSec + alive
	}
	p.n++
	if p.n < p.per {
		return nil, false
	}
	p.scores[p.idx] /= float64(p.per)
	p.idx, p.n = p.idx+1, 0
	if p.idx >= len(p.list) {
		return nil, true
	}
	return &p.list[p.idx], false
}

// Best — стратегия с наименьшей оценкой среди проверенных.
func (p *Picker) Best() Strategy {
	best := 0
	for i := 1; i < len(p.list) && i < p.idx; i++ {
		if p.scores[i] < p.scores[best] {
			best = i
		}
	}
	return p.list[best]
}
