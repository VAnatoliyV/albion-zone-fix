// Пакет app связывает наблюдение, учёт переходов, обход и запись в одно состояние,
// которое показывает окно программы.
package app

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"albionzonefix/internal/bypass"
	"albionzonefix/internal/game"
	"albionzonefix/internal/probe"
	"albionzonefix/internal/record"
	"albionzonefix/internal/zones"
)

const (
	logName      = "переходы.jsonl"
	autoPerStrat = 5
	AutoMode     = "auto"
)

type App struct {
	mu sync.Mutex

	dir     string // где лежат файлы программы (история, записи)
	names   map[string]string
	runner  *bypass.Runner
	strats  []bypass.Strategy
	picker  *bypass.Picker
	pickRes string // итог подбора

	dec     *game.Decoder
	tracker *zones.Tracker
	trs     []zones.Transition
	zone    string

	packets  int
	lastPkt  time.Time
	sniffErr string

	probeRuns []ProbeRun
	probing   bool

	rec      *record.Writer
	recFile  *os.File
	recUntil time.Time
}

func New(dir, binDir string, names map[string]string) *App {
	a := &App{dir: dir, names: names, runner: bypass.NewRunner(binDir),
		strats: bypass.LoadStrategies(filepath.Join(dir, "strategies.json"))}
	a.tracker = zones.NewTracker(a.name, a.strategyNow, a.onTransition)
	a.dec = game.NewDecoder(a.onEvent)
	a.load()
	return a
}

func (a *App) name(code string) string {
	if n := a.names[code]; n != "" {
		return n
	}
	return code
}

func (a *App) strategyNow() string { return a.runner.Current() }

func (a *App) load() {
	f, err := os.Open(filepath.Join(a.dir, logName))
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var tr zones.Transition
		if json.Unmarshal(sc.Bytes(), &tr) == nil {
			a.trs = append(a.trs, tr)
		}
	}
}

// Feed — вход для каждого пакета игры (из драйвера или из записи).
func (a *App) Feed(p game.Packet) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.packets++
	a.lastPkt = p.T
	if a.rec != nil {
		a.rec.Write(p)
	}
	a.dec.Feed(p)
}

func (a *App) onEvent(e game.Ev) {
	if e.Kind == game.Join {
		a.zone = e.Location
	}
	a.tracker.On(e)
}

func (a *App) onTransition(tr zones.Transition) {
	a.trs = append(a.trs, tr)
	if b, err := json.Marshal(tr); err == nil {
		if f, err := os.OpenFile(filepath.Join(a.dir, logName), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			f.Write(append(b, '\n'))
			f.Close()
		}
	}
	if a.picker == nil {
		return
	}
	next, done := a.picker.OnTransition(tr)
	switch {
	case done:
		best := a.picker.Best()
		a.picker = nil
		a.pickRes = "Подбор закончен, лучшая: " + best.Name
		go a.runner.Start(best)
	case next != nil:
		s := *next
		go a.runner.Start(s)
	}
}

// Tick — раз в секунду: таймауты переходов и конец записи.
func (a *App) Tick(now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tracker.Tick(now)
	if a.rec != nil && now.After(a.recUntil) {
		a.stopRecordLocked()
	}
}

func (a *App) SetSniffError(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		a.sniffErr = err.Error()
	} else {
		a.sniffErr = ""
	}
}

func (a *App) findStrategy(name string) (bypass.Strategy, bool) {
	for _, s := range a.strats {
		if s.Name == name {
			return s, true
		}
	}
	return bypass.Strategy{}, false
}

// SetBypass: "off", имя стратегии или AutoMode.
func (a *App) SetBypass(mode string) error {
	a.mu.Lock()
	a.picker, a.pickRes = nil, ""
	a.mu.Unlock()
	switch mode {
	case "off":
		a.runner.Stop()
		return nil
	case AutoMode:
		p := bypass.NewPicker(a.strats, autoPerStrat)
		if err := a.runner.Start(p.Start()); err != nil {
			return err
		}
		a.mu.Lock()
		a.picker = p
		a.mu.Unlock()
		return nil
	}
	s, ok := a.findStrategy(mode)
	if !ok {
		return errors.New("нет такой стратегии: " + mode)
	}
	return a.runner.Start(s)
}

func (a *App) Shutdown() {
	a.runner.Stop()
	a.StopRecord()
}

func (a *App) StartRecord(d time.Duration) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.rec != nil {
		return errors.New("запись уже идёт")
	}
	path := filepath.Join(a.dir, "запись-"+time.Now().Format("2006-01-02_15-04-05")+".azf")
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w, err := record.NewWriter(f)
	if err != nil {
		f.Close()
		return err
	}
	a.rec, a.recFile, a.recUntil = w, f, time.Now().Add(d)
	return nil
}

func (a *App) StopRecord() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stopRecordLocked()
}

func (a *App) stopRecordLocked() {
	if a.rec == nil {
		return
	}
	a.rec.Flush()
	a.recFile.Close()
	a.rec, a.recFile = nil, nil
}

// ProbeRun — один прогон проверки серверов.
type ProbeRun struct {
	T        time.Time      `json:"t"`
	Strategy string         `json:"strategy"`
	OK       int            `json:"ok"`
	Total    int            `json:"total"`
	Results  []probe.Result `json:"results"`
}

// RunProbe запускает проверку серверов в фоне (с текущей стратегией обхода).
func (a *App) RunProbe() error {
	a.mu.Lock()
	if a.probing {
		a.mu.Unlock()
		return errors.New("проверка уже идёт")
	}
	a.probing = true
	a.mu.Unlock()
	go func() {
		run := ProbeRun{T: time.Now(), Strategy: a.runner.Current()}
		run.Results = probe.Run(probe.Targets(), 1500*time.Millisecond)
		run.Total = len(run.Results)
		for _, r := range run.Results {
			if r.OK {
				run.OK++
			}
		}
		if b, err := json.Marshal(run); err == nil {
			if f, err := os.OpenFile(filepath.Join(a.dir, "проверки.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
				f.Write(append(b, '\n'))
				f.Close()
			}
		}
		a.mu.Lock()
		a.probeRuns = append(a.probeRuns, run)
		a.probing = false
		a.mu.Unlock()
	}()
	return nil
}

// State — всё, что показывает окно.
type State struct {
	Packets    int                `json:"packets"`
	LastPacket string             `json:"lastPacket"`
	SniffError string             `json:"sniffError"`
	Zone       string             `json:"zone"`
	Bypass     string             `json:"bypass"`
	BypassErr  string             `json:"bypassError"`
	Auto       string             `json:"auto"`
	Strategies []string           `json:"strategies"`
	Recent     []zones.Transition `json:"recent"`
	WorstOff   []zones.ZoneStat   `json:"worstOff"`
	WorstOn    []zones.ZoneStat   `json:"worstOn"`
	Probing    bool               `json:"probing"`
	ProbeRuns  []ProbeRun         `json:"probeRuns"`
	Recording  string             `json:"recording"`
	RecLeft    int                `json:"recLeft"`
}

func (a *App) State() State {
	cur, berr, _ := a.runner.Status()
	a.mu.Lock()
	defer a.mu.Unlock()
	st := State{Packets: a.packets, SniffError: a.sniffErr, Bypass: cur, BypassErr: berr}
	if !a.lastPkt.IsZero() {
		st.LastPacket = a.lastPkt.Format("15:04:05")
	}
	if a.zone != "" {
		st.Zone = a.name(a.zone)
	}
	for _, s := range a.strats {
		st.Strategies = append(st.Strategies, s.Name)
	}
	if a.picker != nil {
		i, of, n, per := a.picker.Progress()
		st.Auto = fmt.Sprintf("Подбор: стратегия %d из %d, переходов %d из %d", i, of, n, per)
	} else {
		st.Auto = a.pickRes
	}
	n := len(a.trs)
	for i := n - 1; i >= 0 && n-i <= 30; i-- {
		st.Recent = append(st.Recent, a.trs[i])
	}
	st.WorstOff = zones.Worst(a.trs, false)
	st.WorstOn = zones.Worst(a.trs, true)
	st.Probing = a.probing
	for i := len(a.probeRuns) - 1; i >= 0 && len(st.ProbeRuns) < 10; i-- {
		st.ProbeRuns = append(st.ProbeRuns, a.probeRuns[i])
	}
	if a.rec != nil {
		st.Recording = a.recFile.Name()
		st.RecLeft = int(time.Until(a.recUntil).Seconds())
	}
	return st
}
