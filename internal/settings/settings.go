// Пакет settings — настройки Albion Journal, файл JSON в каталоге данных.
package settings

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// FileName — имя файла настроек в каталоге данных.
const FileName = "albion-journal-settings.json"

// Settings — всё, что пользователь включает и выключает. Поля, которых нет
// в файле (старый файл, новая версия), берутся из Default.
type Settings struct {
	// Language — один из i18n.Langs (ru, en, es, pl, de, tr, fr, pt, it);
	// пусто — по языку системы (незнакомый — английский).
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

	// ZoneKey — кнопка карточки зоны: xbutton1 (мышь 4), xbutton2, mbutton,
	// клавиша вида f7 или ctrl+q, или off (internal/hotkey).
	ZoneKey string `json:"zoneKey"`
	// ZoneNotify — прежний переключатель «показывать уведомлением Windows»
	// (до ZoneShow). Читается только для перехода старого файла на
	// ZoneShow; при сохранении равен ZoneShow == ShowNotify.
	ZoneNotify bool `json:"zoneNotify"`
	// ZoneShow — как показывать карточку зоны кроме вкладки (zoneShow у
	// мака): ShowNotify — уведомлением Windows, ShowPanel — панелью поверх
	// игры, ShowOff — никак.
	ZoneShow string `json:"zoneShow"`
	// ZoneOverlaySec — сколько секунд видна панель; ZoneOverlayCorner — в
	// каком углу экрана с игрой (Corner*), как zoneOverlaySec/Corner у мака.
	ZoneOverlaySec    int    `json:"zoneOverlaySec"`
	ZoneOverlayCorner string `json:"zoneOverlayCorner"`
	// Что писать в уведомлении (как notifyChests/notifyRes/notifyDng/
	// notifyPortal/notifyOrder у мака; по умолчанию всё, сундуки сверху).
	NotifyChests bool   `json:"notifyChests"`
	NotifyRes    bool   `json:"notifyRes"`
	NotifyDng    bool   `json:"notifyDng"`
	NotifyPortal bool   `json:"notifyPortal"`
	NotifyOrder  string `json:"notifyOrder"` // chestsFirst или resourcesFirst
	// BlackWarn — предупреждать о чёрном экране: уведомление, когда новый
	// сервер молчит, и риск по истории в карточке портала.
	BlackWarn bool `json:"blackWarn"`
	// ZoneOCRPlain — распознавать только цветной снимок, без серого и
	// инверсии при повторах (на случай, если варианты читают хуже: на
	// настоящих снимках они ещё не проверены). Скрытый ключ, без кнопки.
	ZoneOCRPlain bool `json:"zoneOcrPlain,omitempty"`

	// Skin — оформление страницы: SkinPlain (системный шрифт, мягкие
	// скругления; по умолчанию) или SkinPixel (пиксельный шрифт и срезанные
	// углы), как Skin у мака. В старом файле без ключа — обычное.
	Skin string `json:"skin"`
	// LogoAnim — живой кролик в шапке (logoAnim у мака); выключен — значок.
	// ThemeV2 — разовый переход на обычное оформление уже сделан. В файлах
	// до него skin всегда "pixel" (так писала любая запись настроек с 1.0.2),
	// поэтому при первом чтении без этого ключа ставим обычное; дальше
	// выбор пользователя не трогаем.
	ThemeV2  bool `json:"themeV2"`
	LogoAnim bool `json:"logoAnim"`
	// ResetOnZone — обнулять урон при смене зоны (resetOnZone у мака). Сам
	// сброс делает форк сборщика при входе в зону: он читает файл
	// albion-session-options.json, который пишет программа.
	ResetOnZone bool `json:"resetOnZone"`
	// Вместе с игрой (showWithGame, startWithGame, quitWithGame у мака):
	// показать окно, начать сбор цен, когда игра запустилась; закрыться,
	// когда игра закрылась. Следит internal/gamewatch.
	ShowWithGame  bool `json:"showWithGame"`
	StartWithGame bool `json:"startWithGame"`
	QuitWithGame  bool `json:"quitWithGame"`
}

// Способы показа карточки зоны.
const (
	ShowNotify = "notify"
	ShowPanel  = "panel"
	ShowOff    = "off"
)

// Углы экрана для панели.
const (
	CornerTopRight    = "topRight"
	CornerBottomRight = "bottomRight"
	CornerTopLeft     = "topLeft"
	CornerBottomLeft  = "bottomLeft"
)

// Сколько секунд может висеть панель.
const (
	OverlaySecDefault = 5
	OverlaySecMin     = 2
	OverlaySecMax     = 30
)

// NormalizeShow — знакомый способ показа; всё прочее — уведомление.
func NormalizeShow(s string) string {
	switch s {
	case ShowPanel, ShowOff:
		return s
	}
	return ShowNotify
}

// NormalizeCorner — знакомый угол; всё прочее — правый верхний.
func NormalizeCorner(s string) string {
	switch s {
	case CornerBottomRight, CornerTopLeft, CornerBottomLeft:
		return s
	}
	return CornerTopRight
}

// ClampOverlaySec — секунды панели в разумных пределах (0 — по умолчанию).
func ClampOverlaySec(n int) int {
	switch {
	case n <= 0:
		return OverlaySecDefault
	case n < OverlaySecMin:
		return OverlaySecMin
	case n > OverlaySecMax:
		return OverlaySecMax
	}
	return n
}

// Normalize приводит поля карточки к знакомым значениям, а ZoneNotify — в
// согласие с ZoneShow (его читает прежняя версия, если откатиться).
func (s Settings) Normalize() Settings {
	s.ZoneShow = NormalizeShow(s.ZoneShow)
	s.ZoneNotify = s.ZoneShow == ShowNotify
	s.ZoneOverlaySec = ClampOverlaySec(s.ZoneOverlaySec)
	s.ZoneOverlayCorner = NormalizeCorner(s.ZoneOverlayCorner)
	return s
}

// Оформления страницы.
const (
	SkinPixel = "pixel"
	SkinPlain = "plain"
)

// NormalizeSkin — знакомое оформление; всё прочее — обычное.
func NormalizeSkin(s string) string {
	if s == SkinPixel {
		return SkinPixel
	}
	return SkinPlain
}

// WatchGame — нужно ли следить за процессом игры.
func (s Settings) WatchGame() bool { return s.ShowWithGame || s.StartWithGame || s.QuitWithGame }

// Default — настройки первого запуска (файла настроек ещё нет): работает
// только Авалон — слежка за зоной и отправка дорог на карту. Сбор цен (и с
// ним приёмник), отправка цен в ADP и счётчик фейма выключены, пока
// человек сам их не включит.
func Default() Settings {
	return Settings{StopOnExit: true, AutoUpdate: true, MapSend: true,
		ZoneKey: "xbutton1", ZoneNotify: true, ZoneShow: ShowNotify, ZoneOverlaySec: OverlaySecDefault,
		ZoneOverlayCorner: CornerTopRight, NotifyChests: true, NotifyRes: true, NotifyDng: true, NotifyPortal: true,
		NotifyOrder: "chestsFirst", BlackWarn: true, Skin: SkinPlain, ThemeV2: true, LogoAnim: true}
}

// legacy — основа для уже существующего файла: в прошлых версиях сбор цен,
// ADP и счётчик были включены по умолчанию, и файл прошлой версии без
// этих ключей должен остаться при прежнем выборе.
func legacy() Settings {
	v := Default()
	v.ShareADP, v.SessionStats, v.CollectOnStart = true, true, true
	return v
}

// Store читает и пишет настройки; безопасен из нескольких горутин.
type Store struct {
	mu   sync.Mutex
	path string
	cur  Settings
}

// Open читает файл из dir; нет файла — настройки по умолчанию, битый —
// прежние умолчания (legacy).
// Файла нет (первый запуск) — умолчания сразу записываются: следующая
// версия с другими умолчаниями уже не поменяет выбор этой установки.
func Open(dir string) *Store {
	s := &Store{path: filepath.Join(dir, FileName), cur: Default()}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		s.Set(Default()) // не записалось — умолчания живут до выхода
		return s
	}
	// Файл есть, но не читается или битый — это не новая установка: берём
	// прежние умолчания, чтобы не выключить человеку цены, ADP и счётчик.
	s.cur = legacy()
	if err == nil {
		v := legacy()
		if json.Unmarshal(b, &v) == nil {
			var keys map[string]json.RawMessage
			json.Unmarshal(b, &keys)
			// Файл до способа показа: был переключатель «уведомлением».
			if _, ok := keys["zoneShow"]; !ok {
				v.ZoneShow = ShowOff
				if v.ZoneNotify {
					v.ZoneShow = ShowNotify
				}
			}
			v = v.Normalize()
			s.cur = v
			if keys != nil {
				if _, ok := keys["themeV2"]; !ok {
					v.Skin, v.ThemeV2 = SkinPlain, true
					if s.Set(v) != nil {
						s.cur = v // не записалось — хотя бы на этот запуск
					}
				}
			}
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
