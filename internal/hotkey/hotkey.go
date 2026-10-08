// Пакет hotkey — кнопка карточки зоны: боковая кнопка мыши (по умолчанию
// «назад», XBUTTON1) или F-клавиша. Ловит низкоуровневым хуком Windows
// (WH_MOUSE_LL / WH_KEYBOARD_LL) на своём потоке с циклом сообщений;
// событие не глотает (CallNextHookEx) — игра получает нажатие как обычно.
//
// Здесь — выбор кнопки и проверка события (проверяется на маке); хук — в
// hook_windows.go.
package hotkey

import (
	"strconv"
	"strings"
)

// Key — что нажимать: "xbutton1", "xbutton2", "f1"…"f12" или "off".
type Key string

// Default — кнопка мыши 4 («назад»), как у мака.
const (
	Default Key = "xbutton1"
	Off     Key = "off"
)

// Choices — что можно выбрать в настройках, по порядку.
func Choices() []Key {
	out := []Key{"xbutton1", "xbutton2"}
	for i := 1; i <= 12; i++ {
		out = append(out, Key("f"+strconv.Itoa(i)))
	}
	return append(out, Off)
}

// Valid — знакомая кнопка.
func Valid(k Key) bool {
	for _, c := range Choices() {
		if c == k {
			return true
		}
	}
	return false
}

// Normalize — кнопка из настроек; незнакомая или пустая — по умолчанию.
func Normalize(s string) Key {
	k := Key(strings.ToLower(strings.TrimSpace(s)))
	if !Valid(k) {
		return Default
	}
	return k
}

// Mouse — кнопка мыши (1 — XBUTTON1, 2 — XBUTTON2); 0 — это не мышь.
func (k Key) Mouse() int {
	switch k {
	case "xbutton1":
		return 1
	case "xbutton2":
		return 2
	}
	return 0
}

// VK — виртуальный код F-клавиши (VK_F1 = 0x70); 0 — это не клавиша.
func (k Key) VK() uint32 {
	if !strings.HasPrefix(string(k), "f") {
		return 0
	}
	n, err := strconv.Atoi(string(k[1:]))
	if err != nil || n < 1 || n > 12 {
		return 0
	}
	return 0x70 + uint32(n-1)
}

// Label — подпись кнопки: «мышь 4», «мышь 5» (как пишут игроки), F1…F12.
func (k Key) Label() string {
	switch k.Mouse() {
	case 1:
		return "Mouse 4"
	case 2:
		return "Mouse 5"
	}
	if k.VK() != 0 {
		return strings.ToUpper(string(k))
	}
	return ""
}

// Сообщения хуков.
const (
	wmKeyDown    = 0x0100
	wmSysKeyDown = 0x0104
	wmXButtonDn  = 0x020B
	// llkhfInjected — нажатие подделано программой (SendInput), не человеком.
	llkhfInjected = 0x10
	llmhfInjected = 0x01
)

// MatchMouse — событие мыши msg (wParam хука) с mouseData и flags
// (MSLLHOOKSTRUCT) — нажатие выбранной кнопки. Подделанные нажатия не берём.
func MatchMouse(k Key, msg uintptr, mouseData, flags uint32) bool {
	b := k.Mouse()
	return b != 0 && msg == wmXButtonDn && int(mouseData>>16) == b && flags&llmhfInjected == 0
}

// MatchKey — событие клавиатуры msg с кодом vk и flags (KBDLLHOOKSTRUCT) —
// нажатие выбранной клавиши. Автоповтор отсекает Runner (занят — не берёт).
func MatchKey(k Key, msg uintptr, vk, flags uint32) bool {
	v := k.VK()
	return v != 0 && (msg == wmKeyDown || msg == wmSysKeyDown) && vk == v && flags&llkhfInjected == 0
}
