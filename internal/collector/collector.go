// Пакет collector — разборщик сборщика цен (форк albiondata-client) внутри
// программы: цены для своего приёмника и ADP, счётчик фейма, серебра и урона.
// Пакеты ему отдаёт тот же перехват WinDivert, что и учёту переходов.
package collector

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	client "github.com/ao-data/albiondata-client/client"
)

// ReceiverURL — свой приёмник цен (acp-prices), тот же порт, что у мака.
const ReceiverURL = "http://localhost:7777"

// queueSize — сколько пакетов ждут разбора. Переполнилось — лишнее
// выбрасываем: перехват и учёт переходов ждать разборщик не должны.
const queueSize = 8192

// Config — что сейчас включено.
type Config struct {
	Prices   bool // сбор цен: свой приёмник (+ ADP, если ShareADP)
	ShareADP bool // отправлять цены в Albion Online Data Project
	Session  bool // счётчик фейма, серебра и урона
}

// Running — нужен ли разборщик вообще.
func (c Config) Running() bool { return c.Prices || c.Session }

// Stats — для строки состояния.
type Stats struct {
	Running bool   `json:"running"`
	Session bool   `json:"session"` // счётчик фейма считает
	Fed     int64  `json:"fed"`     // отдано в разборщик
	Dropped int64  `json:"dropped"` // выброшено при переполнении очереди
	Error   string `json:"error,omitempty"`
	// Panics — сколько паник поймано при разборе (пакет пропал, программа
	// жива); LastPanic — последняя. Подробности со стеком — в журнале.
	Panics    int64  `json:"panics"`
	LastPanic string `json:"lastPanic,omitempty"`
}

type Collector struct {
	dataDir, resDir string
	log             io.Writer

	q                    chan []byte
	fed, dropped, panics atomic.Int64
	mu                   sync.Mutex
	running, session     bool
	lastErr, lastPanic   string
}

// New готовит разборщик; запускает его Apply. resDir — где лежит
// items_by_id.json (пусто — рядом с exe). log — журнал клиента.
func New(dataDir, resDir string, log io.Writer) *Collector {
	if log == nil {
		log = io.Discard
	}
	c := &Collector{dataDir: dataDir, resDir: resDir, log: log, q: make(chan []byte, queueSize)}
	go c.loop()
	return c
}

func (c *Collector) loop() {
	for b := range c.q {
		if c.feedOne(b) == nil {
			c.fed.Add(1)
		}
	}
}

// feedOne отдаёт пакет разборщику. Паника внутри не должна ронять
// программу: FeedRawIPv4 ловит свои сам, а это последний рубеж на случай,
// если что-то вылетит снаружи его защиты.
func (c *Collector) feedOne(b []byte) (err error) {
	defer func() {
		if v := recover(); v != nil {
			c.panics.Add(1)
			msg := fmt.Sprintf("паника при разборе пакета: %v", v)
			c.mu.Lock()
			c.lastPanic = msg
			c.mu.Unlock()
			fmt.Fprintf(c.log, "%s %s\n%s\n", time.Now().Format("2006-01-02 15:04:05"), msg, debug.Stack())
			err = errors.New(msg)
		}
	}()
	return feed(b)
}

// feed — вход разборщика; в тестах подменяется.
var feed = client.FeedRawIPv4

// Feed — сырой IPv4-пакет с IP-заголовка. Не блокирует; срез копируется.
func (c *Collector) Feed(b []byte) {
	c.mu.Lock()
	on := c.running
	c.mu.Unlock()
	if !on {
		return
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	select {
	case c.q <- cp:
	default:
		c.dropped.Add(1)
	}
}

// EmbedConfig переводит настройки программы в настройки клиента.
// Сбор цен выключен — обе отправки «в никуда», как у мак-счётчика.
func EmbedConfig(dataDir, resDir string, log io.Writer, cfg Config) client.EmbedConfig {
	ec := client.EmbedConfig{DataDir: dataDir, ResDir: resDir, Log: log, Session: cfg.Session}
	if cfg.Prices {
		ec.PrivateURLs = ReceiverURL
		ec.SharePublic = cfg.ShareADP
	} else {
		ec.PrivateURLs = client.DeadUploader
	}
	return ec
}

// Apply запускает, перенастраивает или останавливает разборщик.
func (c *Collector) Apply(cfg Config) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var err error
	switch {
	case !cfg.Running():
		client.StopEmbedded()
		c.running = false
	case c.running:
		err = client.UpdateEmbedded(EmbedConfig(c.dataDir, c.resDir, c.log, cfg))
	default:
		err = client.StartEmbedded(EmbedConfig(c.dataDir, c.resDir, c.log, cfg))
		c.running = err == nil
	}
	c.session = c.running && cfg.Session
	c.lastErr = ""
	if err != nil {
		c.lastErr = err.Error()
	}
	return err
}

// ResetSession обнуляет счётчик сессии.
func (c *Collector) ResetSession() { client.ResetSession() }

func (c *Collector) Stats() Stats {
	n, last := client.PanicStats()
	c.mu.Lock()
	defer c.mu.Unlock()
	st := Stats{Running: c.running, Session: c.session, Fed: c.fed.Load(), Dropped: c.dropped.Load(), Error: c.lastErr,
		Panics: n + c.panics.Load(), LastPanic: last}
	if c.lastPanic != "" {
		st.LastPanic = c.lastPanic
	}
	return st
}

// SessionFile — файл сессии счётчика (фейм, серебро, урон).
func (c *Collector) SessionFile() string { return filepath.Join(c.dataDir, SessionFileName) }

// SessionFileName — имя файла сессии в каталоге данных (как у мака).
const SessionFileName = "albion-session.json"

// OptionsFileName — настройки счётчика для форка (как у мака): форк читает
// файл при каждом входе в зону (client/session_options.go), поэтому
// перезапускать разборщик ради переключателя не нужно.
const OptionsFileName = "albion-session-options.json"

// WriteOptions пишет настройки счётчика в каталог данных: resetOnZone —
// обнулять урон при смене зоны. Через временный файл, чтобы форк не
// прочитал полфайла.
func WriteOptions(dataDir string, resetOnZone bool) error {
	b, err := json.Marshal(map[string]bool{"resetOnZone": resetOnZone})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dataDir, OptionsFileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// Close останавливает разборщик (при выходе).
func (c *Collector) Close() {
	c.Apply(Config{})
}
