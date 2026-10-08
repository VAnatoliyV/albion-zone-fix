// Пакет app связывает наблюдение, учёт переходов, обход и запись в одно состояние,
// которое показывает окно программы.
package app

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"albionzonefix/internal/avalon"
	"albionzonefix/internal/bypass"
	"albionzonefix/internal/collector"
	"albionzonefix/internal/game"
	"albionzonefix/internal/hotkey"
	"albionzonefix/internal/probe"
	"albionzonefix/internal/record"
	"albionzonefix/internal/settings"
	"albionzonefix/internal/trace"
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
	binDir    string
	traces    []TraceRun
	tracing   string // что сейчас трассируем

	rec      *record.Writer
	recFile  *os.File
	recUntil time.Time

	settings   *settings.Store
	col        Collector // разборщик цен и счётчика; nil — не подключён
	collecting bool      // сбор цен идёт сейчас (включается кнопкой или при открытии)
	colMu      sync.Mutex
	rcv        Receiver // приёмник своих цен; nil — не подключён

	// Карта Авалона: где игрок (из живого потока Join), отправка проходов.
	here      *avalon.Place // nil — после запуска входа в зону ещё не видели
	hereAt    time.Time
	leaves    leaveCounter // уходы из локации с последнего Join
	away      bool         // ушли из локации, Join нет дольше awayAfter: место неизвестно
	mapRep    MapReporter
	onZone    func(code string) // зона сменилась (подсветить на открытой карте)
	installMu sync.Mutex

	// Карточка зоны: последнее нажатие и куда человек собрался.
	card     *cardState
	expect   *expectation
	cardInfo CardInfo

	log func(format string, args ...any) // журнал программы; nil — молча
}

// MapReporter — отправка проходов на карту (avalon.Reporter); в тестах подделка.
type MapReporter interface {
	Offer(avalon.Pass)
	Note(avalon.Pass, string)
	Last() *avalon.Status
	OfferTip(avalon.Tip)
	LastTip() *avalon.Status
}

// Receiver — запуск и остановка приёмника своих цен (receiver.Manager);
// в тестах подделка.
type Receiver interface {
	Start() error
	Stop()
	Up() bool
	Err() string
	Keep(bool)
}

// Collector — разборщик форка сборщика (collector.Collector); в тестах подделка.
type Collector interface {
	Apply(collector.Config) error
	ResetSession()
	Stats() collector.Stats
}

func New(dir, binDir string, names map[string]string) *App {
	a := &App{dir: dir, binDir: binDir, names: names, runner: bypass.NewRunner(binDir),
		strats: bypass.LoadStrategies(filepath.Join(dir, "strategies.json")), settings: settings.Open(dir)}
	a.collecting = a.settings.Get().CollectOnStart
	a.tracker = zones.NewTracker(a.name, a.strategyNow, a.onTransition)
	a.dec = game.NewDecoder(a.onEvent)
	a.load()
	// Сброс урона при смене зоны делает форк по своему файлу: файл мог
	// остаться от другой версии или пропасть — пишем по настройкам сразу.
	a.writeOptions(a.settings.Get().ResetOnZone)
	return a
}

func (a *App) name(code string) string {
	if n := a.names[code]; n != "" {
		return n
	}
	if z, ok := avalon.Lookup(code); ok && z.Name != "" {
		return z.Name // дороги Авалона: в старом справочнике их нет
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
	n := a.leaves.on(e)
	if e.Kind == game.Join {
		a.zone = e.Location
		a.onJoin(e, n)
	}
	a.tracker.On(e)
}

// onJoin — вход в зону: текущее место и проход для карты Авалона. Зона —
// из того же потока пакетов, что учёт переходов (без файлов и процессов);
// первый вход после запуска проходом не считается. Зовётся под a.mu,
// поэтому всё, что дальше, не блокирует.
//
// leaves — уходов из локации с прошлого Join. Больше одного — между ними
// была зона без Join (Туманы): откуда пришли, неизвестно, это не проход.
func (a *App) onJoin(e game.Ev, leaves int) {
	cur := avalon.Place{Zone: e.Location, Region: avalon.Region(e.Server)}
	wasAway := a.away
	a.away = false
	if leaves > 1 && a.here != nil {
		a.logfLocked("место: %d ухода без входа с %s — вход в %s не проход", leaves, a.here.Zone, cur.Zone)
		a.here = nil
	}
	p, why := avalon.Decide(a.here, cur)
	if a.here == nil || a.here.Zone != cur.Zone || wasAway {
		a.hereAt = e.T
		if a.onZone != nil {
			a.onZone(cur.Zone)
		}
	}
	a.here = &cur
	if a.mapRep == nil || !a.settings.Get().MapSend {
		return
	}
	switch why {
	case avalon.OK:
		a.mapRep.Offer(p)
	case avalon.NotEurope:
		a.mapRep.Note(p, avalon.ResRegion)
	}
}

// AttachMap подключает отправку проходов и подсветку зоны на карте.
// Звать до начала перехвата.
func (a *App) AttachMap(r MapReporter, onZone func(code string)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mapRep, a.onZone = r, onZone
}

// HereCode — код текущей зоны ("" — неизвестна).
func (a *App) HereCode() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.here == nil || a.away {
		return ""
	}
	return a.here.Zone
}

// placeLocked — где игрок на момент now: nil — неизвестно (входа не видели
// или ушли из локации и Join нет дольше awayAfter). Зовётся под a.mu.
func (a *App) placeLocked(now time.Time) *avalon.Place {
	if a.here == nil || a.here.Zone == "" || a.away || a.leaves.away(now) {
		return nil
	}
	h := *a.here
	return &h
}

// checkAway — ушли из локации и Join нет дольше awayAfter: место
// неизвестно (вкладка «Не знаю, где ты», подсветка на карте снимается).
// Зовётся под a.mu из Tick и CheckStall.
func (a *App) checkAway(now time.Time) {
	if a.away || a.here == nil || !a.leaves.away(now) {
		return
	}
	a.away = true
	if a.onZone != nil {
		a.onZone("")
	}
}

// MapInstall — номер установки для сервера карты; при первом вызове
// создаётся и сохраняется в настройках.
func (a *App) MapInstall() string {
	a.installMu.Lock()
	defer a.installMu.Unlock()
	s := a.settings.Get()
	if s.MapInstall != "" {
		return s.MapInstall
	}
	s.MapInstall = avalon.NewInstall()
	a.settings.Set(s) // не сохранилось — номер живёт до выхода
	return s.MapInstall
}

func (a *App) onTransition(tr zones.Transition) {
	if w := a.wantFor(tr); w != "" {
		tr.Want = w
	}
	a.expect = nil // переход случился — прежний снимок портала больше не цель
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
	a.checkAway(now)
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

// ErrBypassDisabled — встроенный обход выключен в настройках.
var ErrBypassDisabled = errors.New("встроенный обход выключен в настройках")

// SetBypass: "off", имя стратегии или AutoMode. Включить обход можно, только
// если он разрешён в настройках (BuiltinBypass).
func (a *App) SetBypass(mode string) error {
	if mode != "off" && !a.settings.Get().BuiltinBypass {
		return ErrBypassDisabled
	}
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
	a.colMu.Lock()
	if a.col != nil {
		a.col.Apply(collector.Config{})
	}
	a.colMu.Unlock()
	// Приёмник и сайт: «останавливать всё при выходе» выключено — оставляем
	// работать (он тогда запущен вне объекта задания, см. receiver.Manager.Keep).
	if a.rcv != nil && a.settings.Get().StopOnExit {
		a.rcv.Stop()
	}
}

// AttachReceiver подключает приёмник. Сам он стартует вместе со сбором цен
// (AttachCollector, SetCollecting) или по кнопке (SetReceiver).
func (a *App) AttachReceiver(r Receiver) {
	r.Keep(!a.settings.Get().StopOnExit)
	a.colMu.Lock()
	a.rcv = r
	a.colMu.Unlock()
}

// startReceiverIfCollecting поднимает приёмник, если сбор цен включён.
func (a *App) startReceiverIfCollecting() {
	a.colMu.Lock()
	r := a.rcv
	a.colMu.Unlock()
	if r != nil && a.Collecting() {
		r.Start() // причина неудачи — в r.Err(), её показывает строка состояния
	}
}

// SetReceiver — кнопка «Приёмник»: запустить или остановить.
func (a *App) SetReceiver(on bool) error {
	a.colMu.Lock()
	r := a.rcv
	a.colMu.Unlock()
	if r == nil {
		return errors.New("приёмник не подключён")
	}
	if on {
		return r.Start()
	}
	r.Stop()
	return nil
}

// AttachCollector подключает разборщик и включает его по настройкам.
func (a *App) AttachCollector(c Collector) error {
	a.colMu.Lock()
	a.col = c
	a.colMu.Unlock()
	a.startReceiverIfCollecting()
	return a.applyCollector()
}

// collectorConfig — что должно работать при текущих настройках.
func (a *App) collectorConfig() collector.Config {
	s := a.settings.Get()
	a.mu.Lock()
	on := a.collecting
	a.mu.Unlock()
	return collector.Config{Prices: on, ShareADP: s.ShareADP, Session: s.SessionStats}
}

func (a *App) applyCollector() error {
	a.colMu.Lock()
	defer a.colMu.Unlock()
	if a.col == nil {
		return nil
	}
	// Настройки читаем под colMu: два переключения подряд не применятся задом наперёд.
	return a.col.Apply(a.collectorConfig())
}

// Settings — текущие настройки программы.
func (a *App) Settings() settings.Settings { return a.settings.Get() }

// SetSettings сохраняет настройки и сразу применяет их к разборщику.
//
// Встроенный обход запретили — работающий winws гасится сразу.
func (a *App) SetSettings(s settings.Settings) error {
	// Кнопку и порядок строк уведомления — только знакомые.
	s.ZoneKey = string(hotkey.Normalize(s.ZoneKey))
	if s.NotifyOrder != "resourcesFirst" {
		s.NotifyOrder = "chestsFirst"
	}
	s.Skin = settings.NormalizeSkin(s.Skin)
	s = s.Normalize() // способ показа карточки, угол и секунды панели
	// Номер установки страница не меняет: берём сохранённый.
	a.installMu.Lock()
	s.MapInstall = a.settings.Get().MapInstall
	s.ThemeV2 = true // переход на обычное оформление уже сделан (settings.Open)
	err := a.settings.Set(s)
	a.installMu.Unlock()
	if err != nil {
		return err
	}
	if !s.BuiltinBypass {
		a.mu.Lock()
		a.picker, a.pickRes = nil, ""
		a.mu.Unlock()
		a.runner.Stop()
	}
	a.colMu.Lock()
	r := a.rcv
	a.colMu.Unlock()
	if r != nil {
		r.Keep(!s.StopOnExit)
	}
	err = a.applyCollector()
	// Настройки уже сохранены: неудача с файлом для форка (его держит
	// открытым сам форк, редкий случай) — только в журнал, не повод
	// говорить странице «не сохранилось» и пропускать хук настроек.
	a.writeOptions(s.ResetOnZone)
	return err
}

// writeOptions — файл настроек счётчика для форка (resetOnZone).
func (a *App) writeOptions(resetOnZone bool) {
	if err := collector.WriteOptions(a.dir, resetOnZone); err != nil {
		a.logf("сброс урона при смене зоны не передан счётчику: %v", err)
	}
}

// SetLog — куда писать то, что не показывается на странице (журнал программы).
func (a *App) SetLog(logf func(format string, args ...any)) {
	a.mu.Lock()
	a.log = logf
	a.mu.Unlock()
}

func (a *App) logf(format string, args ...any) {
	a.mu.Lock()
	l := a.log
	a.mu.Unlock()
	if l != nil {
		l(format, args...)
	}
}

// logfLocked — то же под a.mu (журнал не берёт a.mu).
func (a *App) logfLocked(format string, args ...any) {
	if a.log != nil {
		a.log(format, args...)
	}
}

// SetCollecting запускает или останавливает сбор цен (не трогая счётчик).
func (a *App) SetCollecting(on bool) error {
	a.mu.Lock()
	a.collecting = on
	a.mu.Unlock()
	if on {
		a.startReceiverIfCollecting()
	}
	return a.applyCollector()
}

// EnableCollecting — кнопка «Включить» у сбора цен: включает сбор сейчас
// и запоминает «начинать сбор при открытии», как «Включить» у счётчика.
// Остановка кнопкой — только до выхода (SetCollecting(false)).
func (a *App) EnableCollecting() error {
	if s := a.settings.Get(); !s.CollectOnStart {
		s.CollectOnStart = true
		if err := a.SetSettings(s); err != nil {
			return err
		}
	}
	return a.SetCollecting(true)
}

// Collecting — идёт ли сбор цен.
func (a *App) Collecting() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.collecting
}

// ResetSession обнуляет счётчик фейма, серебра и урона.
func (a *App) ResetSession() error {
	a.colMu.Lock()
	defer a.colMu.Unlock()
	if a.col == nil {
		return errors.New("счётчик не подключён")
	}
	a.col.ResetSession()
	return nil
}

func (a *App) collectorStats() collector.Stats {
	a.colMu.Lock()
	defer a.colMu.Unlock()
	if a.col == nil {
		return collector.Stats{}
	}
	return a.col.Stats()
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

// TraceRun — трассировка до одного сервера.
type TraceRun struct {
	T        time.Time   `json:"t"`
	Server   string      `json:"server"`
	Why      string      `json:"why"`
	Strategy string      `json:"strategy"`
	Reached  bool        `json:"reached"`
	Hops     []trace.Hop `json:"hops"`
	Error    string      `json:"error,omitempty"`
}

// traceTargets: сервер последнего неудачного перехода и сервер, который отвечал.
func (a *App) traceTargets() [][2]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	bad := ""
	for i := len(a.trs) - 1; i >= 0; i-- {
		if !a.trs[i].Replied && a.trs[i].Server != "" {
			bad = a.trs[i].Server
			break
		}
	}
	why := "сервер последнего неудачного перехода"
	if bad == "" {
		bad, why = "193.169.238.242:5056", "сервер Лимхёрста из записи (win-22)"
	}
	good := ""
	if n := len(a.probeRuns); n > 0 {
		for _, r := range a.probeRuns[n-1].Results {
			if r.OK && r.Addr != bad {
				good = r.Addr
				break
			}
		}
	}
	gwhy := "сервер, ответивший в последней проверке"
	if good == "" {
		good, gwhy = "193.169.238.102:5056", "сервер, с которым шла игра в записи (win-08)"
	}
	return [][2]string{{bad, why}, {good, gwhy}}
}

// RunTrace трассирует два сервера по очереди в фоне.
func (a *App) RunTrace() error {
	a.mu.Lock()
	if a.tracing != "" {
		a.mu.Unlock()
		return errors.New("трассировка уже идёт")
	}
	a.tracing = "…"
	a.mu.Unlock()
	go func() {
		defer func() { a.mu.Lock(); a.tracing = ""; a.mu.Unlock() }()
		icmp, closeICMP, err := trace.OpenICMP(a.binDir)
		for _, t := range a.traceTargets() {
			a.mu.Lock()
			a.tracing = t[0]
			a.mu.Unlock()
			run := TraceRun{T: time.Now(), Server: t[0], Why: t[1], Strategy: a.runner.Current()}
			if err != nil {
				run.Error = "не удалось слушать ICMP: " + err.Error()
			} else if dst, perr := netip.ParseAddrPort(t[0]); perr != nil {
				run.Error = perr.Error()
			} else if s, serr := trace.NewUDPSender(dst); serr != nil {
				run.Error = serr.Error()
			} else {
				run.Hops = trace.Run(s, dst, icmp, s.UDP, trace.Options{MaxTTL: 30, Wait: 1500 * time.Millisecond, StopAfterSilent: 5})
				s.Close()
				run.Reached = trace.Reached(run.Hops)
				for i := range run.Hops {
					run.Hops[i].Name = reverseName(run.Hops[i].IP)
				}
			}
			if b, jerr := json.Marshal(run); jerr == nil {
				if f, ferr := os.OpenFile(filepath.Join(a.dir, "трассировки.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); ferr == nil {
					f.Write(append(b, '\n'))
					f.Close()
				}
			}
			a.mu.Lock()
			a.traces = append(a.traces, run)
			a.mu.Unlock()
		}
		if closeICMP != nil {
			closeICMP()
		}
	}()
	return nil
}

// reverseName — обратное имя узла (по нему видно провайдера и город), не дольше секунды.
func reverseName(ip string) string {
	if ip == "" {
		return ""
	}
	ch := make(chan string, 1)
	go func() {
		names, _ := net.LookupAddr(ip)
		if len(names) > 0 {
			ch <- strings.TrimSuffix(names[0], ".")
		} else {
			ch <- ""
		}
	}()
	select {
	case n := <-ch:
		return n
	case <-time.After(time.Second):
		return ""
	}
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
	Tracing    string             `json:"tracing"`
	Traces     []TraceRun         `json:"traces"`
	Probing    bool               `json:"probing"`
	ProbeRuns  []ProbeRun         `json:"probeRuns"`
	Recording  string             `json:"recording"`
	RecLeft    int                `json:"recLeft"`
	DataDir    string             `json:"dataDir"`
	Collecting bool               `json:"collecting"`
	Settings   settings.Settings  `json:"settings"`
	Collector  collector.Stats    `json:"collector"`
	// ReceiverErr — почему приёмник не работает (порт занят, нет файла, упал).
	ReceiverErr string `json:"receiverError"`
	// Here — текущая зона для вкладки «Зона»; nil — входа в зону не видели.
	Here *Here `json:"here,omitempty"`
	// MapLast — последний проход по дорогам и итог отправки на карту.
	MapLast *avalon.Status `json:"mapLast,omitempty"`
	// Card — карточка зоны по кнопке (этап 4).
	Card *CardView `json:"card"`
}

// Here — где игрок сейчас.
type Here struct {
	avalon.Zone
	Known  bool      `json:"known"`  // есть в справочнике зон
	Region string    `json:"region"` // europe, americas, asia или ""
	Since  time.Time `json:"since"`
}

func (a *App) State() State {
	cur, berr, _ := a.runner.Status()
	cs, set := a.collectorStats(), a.settings.Get()
	a.colMu.Lock()
	rcv := a.rcv
	a.colMu.Unlock()
	a.mu.Lock()
	mr := a.mapRep
	a.mu.Unlock()
	var mapLast, tipLast *avalon.Status
	if mr != nil {
		mapLast, tipLast = mr.Last(), mr.LastTip()
	}
	rerr := ""
	if rcv != nil {
		rerr = rcv.Err()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	st := State{Packets: a.packets, SniffError: a.sniffErr, Bypass: cur, BypassErr: berr,
		DataDir: a.dir, Collecting: a.collecting, Settings: set, Collector: cs, ReceiverErr: rerr, MapLast: mapLast}
	st.Card = a.cardView(tipLast, set.BlackWarn)
	switch {
	case a.here != nil && a.away:
		// Ушли из локации, а входа нет: «не знаю, где ты» с момента ухода.
		st.Here = &Here{Since: a.leaves.leftAt}
	case a.here != nil:
		z, ok := avalon.Lookup(a.here.Zone)
		if !ok {
			z = avalon.Zone{Code: a.here.Zone, Name: a.name(a.here.Zone)}
		}
		st.Here = &Here{Zone: z, Known: ok, Region: a.here.Region, Since: a.hereAt}
	}
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
	st.Tracing = a.tracing
	for i := len(a.traces) - 1; i >= 0 && len(st.Traces) < 4; i-- {
		st.Traces = append(st.Traces, a.traces[i])
	}
	for i := len(a.probeRuns) - 1; i >= 0 && len(st.ProbeRuns) < 10; i-- {
		st.ProbeRuns = append(st.ProbeRuns, a.probeRuns[i])
	}
	if a.rec != nil {
		st.Recording = a.recFile.Name()
		st.RecLeft = int(time.Until(a.recUntil).Seconds())
	}
	return st
}
