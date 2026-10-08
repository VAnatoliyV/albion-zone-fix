// Пакет desktop — окно программы (WebView2), значок в трее и мелочи
// рабочего стола Windows: открыть адрес в браузере, папку в Проводнике,
// сообщение, одна копия программы. Всё, что зависит от Windows, — в
// *_windows.go; на маке (разработка) окно заменяет обычный браузер.
package desktop

import (
	_ "embed"
	"sync"
)

// Icon — кролик, значок окна и трея (.ico: 16…256, PNG внутри).
//
//go:embed rabbit.ico
var Icon []byte

// Config — что нужно окну и трею от программы.
type Config struct {
	URL     string // страница программы на 127.0.0.1
	Title   string
	Hidden  bool   // запуск в трей без окна (автозапуск)
	DataDir string // папка данных: профиль WebView2, файл значка

	// Надписи трея на текущем языке: ключ словаря → строка.
	Label func(key string) string
	// Сбор цен из трея.
	Collecting    func() bool
	SetCollecting func(on bool) error
	// EndSession — Windows выключается или пользователь выходит: успеть
	// сохранить сессию и остановить обход. Может вызываться дважды.
	EndSession func()
	// Logf — журнал программы.
	Logf func(format string, args ...any)
}

// Desktop — окно и трей. Show, Quit и Relabel можно звать из любой горутины.
type Desktop struct {
	cfg Config

	mu       sync.Mutex
	quit     bool
	fallback bool // WebView2 не создалось — показываем в браузере
	native   nativeState
}

// New готовит окно; показывает его Run.
func New(cfg Config) *Desktop {
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	if cfg.Label == nil {
		cfg.Label = func(k string) string { return k }
	}
	if cfg.Title == "" {
		cfg.Title = "Albion Journal"
	}
	return &Desktop{cfg: cfg}
}

// Fallback — окно открыто в браузере, а не во встроенном WebView2.
func (d *Desktop) Fallback() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.fallback
}
