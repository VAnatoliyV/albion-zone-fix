// Пакет settings — настройки Albion Journal, файл JSON в каталоге данных.
package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// FileName — имя файла настроек в каталоге данных.
const FileName = "albion-journal-settings.json"

// Settings — всё, что пользователь включает и выключает. Поля, которых нет
// в файле (старый файл, новая версия), берутся из Default.
type Settings struct {
	// Language — "ru", "en", "es"; пусто — по языку системы.
	Language string `json:"language"`
	// ShareADP — отправлять цены в Albion Online Data Project.
	ShareADP bool `json:"shareADP"`
	// SessionStats — счётчик фейма, серебра и урона.
	SessionStats bool `json:"sessionStats"`
	// CollectOnStart — начинать сбор цен сразу при открытии программы.
	CollectOnStart bool `json:"collectOnStart"`
	// StopOnExit — при выходе останавливать всё (сбор, приёмник, обход).
	StopOnExit bool `json:"stopOnExit"`
	// StartWithWindows — запускать программу вместе с Windows (задача
	// Планировщика с наивысшими правами, см. internal/autostart).
	StartWithWindows bool `json:"startWithWindows"`
	// BuiltinBypass — разрешить встроенный обход Zone Fix (winws). По
	// умолчанию выключен: у многих игроков в России уже стоит свой zapret,
	// и два обхода на одном трафике мешают друг другу.
	BuiltinBypass bool `json:"builtinBypass"`
	// AutoUpdate — проверять выпуски сам и ставить скачанное при выходе.
	// Выключено — только кнопкой «Проверить обновления» и «Перезапустить сейчас».
	AutoUpdate bool `json:"autoUpdate"`
	// MapSend — отправлять проходы по дорогам Авалона на общую карту.
	MapSend bool `json:"mapSend"`
	// MapInstall — случайный номер установки для сервера карты (лимиты и
	// бан). Создаётся при первой отправке; страница его не меняет.
	MapInstall string `json:"mapInstall,omitempty"`
}

// Default — настройки первого запуска.
func Default() Settings {
	return Settings{ShareADP: true, SessionStats: true, CollectOnStart: true, StopOnExit: true, AutoUpdate: true, MapSend: true}
}

// Store читает и пишет настройки; безопасен из нескольких горутин.
type Store struct {
	mu   sync.Mutex
	path string
	cur  Settings
}

// Open читает файл из dir; нет файла или он битый — настройки по умолчанию.
func Open(dir string) *Store {
	s := &Store{path: filepath.Join(dir, FileName), cur: Default()}
	if b, err := os.ReadFile(s.path); err == nil {
		v := Default()
		if json.Unmarshal(b, &v) == nil {
			s.cur = v
		}
	}
	return s
}

// Get — текущие настройки.
func (s *Store) Get() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur
}

// Path — где лежит файл.
func (s *Store) Path() string { return s.path }

// Set запоминает и сохраняет настройки (через временный файл, чтобы
// обрыв записи не оставил полфайла).
func (s *Store) Set(v Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return err
	}
	s.cur = v
	return nil
}
