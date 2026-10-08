// Пакет collector — разборщик сборщика цен (форк albiondata-client) внутри
// программы: цены для своего приёмника и ADP, счётчик фейма, серебра и урона.
// Пакеты ему отдаёт тот же перехват WinDivert, что и учёту переходов.
package collector

import (
	"io"
	"sync"
	"sync/atomic"

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
	Fed     int64  `json:"fed"`     // отдано в разборщик
	Dropped int64  `json:"dropped"` // выброшено при переполнении очереди
	Error   string `json:"error,omitempty"`
}

type Collector struct {
	dataDir, resDir string
	log             io.Writer

	q            chan []byte
	fed, dropped atomic.Int64
	mu           sync.Mutex
	running      bool
	lastErr      string
}

// New готовит разборщик; запускает его Apply. resDir — где лежит
// items_by_id.json (пусто — рядом с exe). log — журнал клиента.
func New(dataDir, resDir string, log io.Writer) *Collector {
	c := &Collector{dataDir: dataDir, resDir: resDir, log: log, q: make(chan []byte, queueSize)}
	go c.loop()
	return c
}

func (c *Collector) loop() {
	for b := range c.q {
		if client.FeedRawIPv4(b) == nil {
			c.fed.Add(1)
		}
	}
}

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
	c.lastErr = ""
	if err != nil {
		c.lastErr = err.Error()
	}
	return err
}

// ResetSession обнуляет счётчик сессии.
func (c *Collector) ResetSession() { client.ResetSession() }

func (c *Collector) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Stats{Running: c.running, Fed: c.fed.Load(), Dropped: c.dropped.Load(), Error: c.lastErr}
}

// Close останавливает разборщик (при выходе).
func (c *Collector) Close() {
	c.Apply(Config{})
}
