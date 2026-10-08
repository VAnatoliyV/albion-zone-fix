// Пакет hotkey — кнопка карточки зоны: кнопка мыши (средняя, «назад» —
// XBUTTON1 по умолчанию, «вперёд») или клавиша с модификаторами (Ctrl+Q),
// голые F1–F24 и несколько служебных клавиш. Кнопку назначают как на маке:
// «Нажми кнопку…» и следующее нажатие (Record). Ловит низкоуровневыми
// хуками Windows (WH_MOUSE_LL / WH_KEYBOARD_LL) на своём потоке с циклом
// сообщений; событие не глотает (CallNextHookEx) — игра получает нажатие
// как обычно.
//
// Здесь — запись кнопки строкой, решение «годится ли нажатие» при записи и
// совпадение нажатия с кнопкой (проверяется на маке); хуки — в
// hook_windows.go.
package hotkey

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Key — кнопка строкой, как в настройках: "xbutton1", "xbutton2", "mbutton",
// "f5", "ctrl+q", "ctrl+shift+f2", "alt+vkc0" или "off". Модификаторы — в
// порядке ctrl, alt, shift, win; клавиша — последней.
type Key string

// Default — кнопка мыши 4 («назад»), как у мака.
const (
	Default Key = "xbutton1"
	Off     Key = "off"
)

// Модификаторы.
const (
	ModCtrl uint8 = 1 << iota
	ModAlt
	ModShift
	ModWin
	// ModAltGr — нажат AltGr (правый Alt на европейских раскладках): Windows
	// подмешивает к нему левый Ctrl, но это не Ctrl+Alt, а набор символа
	// (AltGr+2 — «@» в испанской). Ctrl и Alt при нём не считаются.
	ModAltGr
)

// realMods — модификаторы без AltGr и подмешанных им Ctrl+Alt.
func realMods(m uint8) uint8 {
	if m&ModAltGr != 0 {
		m &^= ModCtrl | ModAlt
	}
	return m &^ ModAltGr
}

var modNames = []struct {
	bit  uint8
	name string
}{{ModCtrl, "ctrl"}, {ModAlt, "alt"}, {ModShift, "shift"}, {ModWin, "win"}}

// Кнопки мыши (Combo.Mouse).
const (
	MouseX1     = 1 // XBUTTON1, «назад», мышь 4
	MouseX2     = 2 // XBUTTON2, «вперёд», мышь 5
	MouseMiddle = 3 // средняя (колесо)
)

// Combo — разобранная кнопка: либо мышь, либо клавиша VK с модификаторами.
type Combo struct {
	Mouse int
	VK    uint32
	Mods  uint8
}

// Виртуальные коды, которые нужны по имени.
const (
	vkEsc      = 0x1B
	vkF1       = 0x70
	vkF24      = 0x87
	vkInsert   = 0x2D
	vkPageUp   = 0x21
	vkPageDown = 0x22
	vkPause    = 0x13
	vkScroll   = 0x91
)

// keyNames — имена клавиш в строке кнопки (кроме букв, цифр и F-клавиш).
var keyNames = map[uint32]string{
	0x03: "break", 0x08: "backspace", 0x09: "tab", 0x0D: "enter", vkPause: "pause", 0x14: "capslock",
	vkEsc: "esc", 0x20: "space", vkPageUp: "pageup", vkPageDown: "pagedown", 0x23: "end", 0x24: "home",
	0x25: "left", 0x26: "up", 0x27: "right", 0x28: "down", vkInsert: "insert", 0x2E: "delete",
	0x6A: "num*", 0x6B: "num+", 0x6D: "num-", 0x6E: "num.", 0x6F: "num/", 0x90: "numlock", vkScroll: "scrolllock",
}

var keyByName = func() map[string]uint32 {
	m := map[string]uint32{}
	for vk, n := range keyNames {
		m[n] = vk
	}
	return m
}()

func vkName(vk uint32) string {
	switch {
	case vk >= 'A' && vk <= 'Z', vk >= '0' && vk <= '9':
		return strings.ToLower(string(rune(vk)))
	case vk >= vkF1 && vk <= vkF24:
		return "f" + strconv.Itoa(int(vk-vkF1+1))
	case vk >= 0x60 && vk <= 0x69:
		return "num" + strconv.Itoa(int(vk-0x60))
	}
	if n, ok := keyNames[vk]; ok {
		return n
	}
	return fmt.Sprintf("vk%02x", vk)
}

func vkByName(n string) uint32 {
	if len(n) == 1 && (n[0] >= 'a' && n[0] <= 'z' || n[0] >= '0' && n[0] <= '9') {
		return uint32(strings.ToUpper(n)[0])
	}
	if vk, ok := keyByName[n]; ok {
		return vk
	}
	if strings.HasPrefix(n, "f") {
		if i, err := strconv.Atoi(n[1:]); err == nil && i >= 1 && i <= 24 && n[1] != '0' {
			return vkF1 + uint32(i-1)
		}
	}
	if strings.HasPrefix(n, "num") {
		if i, err := strconv.Atoi(n[3:]); err == nil && len(n) == 4 && i >= 0 && i <= 9 {
			return 0x60 + uint32(i)
		}
	}
	if strings.HasPrefix(n, "vk") && len(n) == 4 {
		if v, err := strconv.ParseUint(n[2:], 16, 8); err == nil && v > 0 && !isModifierVK(uint32(v)) {
			return uint32(v)
		}
	}
	return 0
}

// isModifierVK — сама клавиша-модификатор (Shift, Ctrl, Alt, Win, левые и
// правые): кнопкой она быть не может, при записи ждём следующую.
func isModifierVK(vk uint32) bool {
	switch vk {
	case 0x10, 0x11, 0x12, 0xA0, 0xA1, 0xA2, 0xA3, 0xA4, 0xA5, 0x5B, 0x5C:
		return true
	}
	return false
}

// BareOK — клавиша годится без модификатора: в чате игры её не набирают.
// F1–F24, Insert, Page Up/Down, Pause, Scroll Lock. Home и End — нет: ими
// ходят по строке чата; буквы, цифры, пробел, Enter, стрелки — тем более.
func BareOK(vk uint32) bool {
	switch {
	case vk >= vkF1 && vk <= vkF24:
		return true
	case vk == vkInsert, vk == vkPageUp, vk == vkPageDown, vk == vkPause, vk == vkScroll:
		return true
	}
	return false
}

// String — кнопка строкой для настроек.
func (c Combo) String() Key {
	switch c.Mouse {
	case MouseX1:
		return "xbutton1"
	case MouseX2:
		return "xbutton2"
	case MouseMiddle:
		return "mbutton"
	}
	if c.VK == 0 {
		return Off
	}
	var b strings.Builder
	for _, m := range modNames {
		if c.Mods&m.bit != 0 {
			b.WriteString(m.name + "+")
		}
	}
	b.WriteString(vkName(c.VK))
	return Key(b.String())
}

// Parse разбирает кнопку; ok=false — незнакомая строка или кнопка, которой
// быть нельзя (голая буква, левая кнопка мыши). "off" — не кнопка.
func Parse(s string) (Combo, bool) {
	k := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
	switch k {
	case "xbutton1":
		return Combo{Mouse: MouseX1}, true
	case "xbutton2":
		return Combo{Mouse: MouseX2}, true
	case "mbutton":
		return Combo{Mouse: MouseMiddle}, true
	case "", "off":
		return Combo{}, false
	}
	parts := strings.Split(k, "+")
	// «num+» — клавиша с плюсом: последний кусок пустой.
	if len(parts) >= 2 && parts[len(parts)-1] == "" && strings.HasSuffix(parts[len(parts)-2], "num") {
		parts = append(parts[:len(parts)-2], parts[len(parts)-2]+"+")
	}
	var c Combo
	for _, p := range parts[:len(parts)-1] {
		found := false
		for _, m := range modNames {
			if p == m.name && c.Mods&m.bit == 0 {
				c.Mods |= m.bit
				found = true
			}
		}
		if !found {
			return Combo{}, false
		}
	}
	c.VK = vkByName(parts[len(parts)-1])
	if c.VK == 0 || c.VK == vkEsc || (c.Mods == 0 && !BareOK(c.VK)) {
		return Combo{}, false
	}
	return c, true
}

// Normalize — кнопка из настроек в каноническом виде; "off" — выключена;
// незнакомая, пустая или негодная (голая буква) — по умолчанию.
func Normalize(s string) Key {
	if strings.EqualFold(strings.TrimSpace(s), string(Off)) {
		return Off
	}
	c, ok := Parse(s)
	if !ok {
		return Default
	}
	return c.String()
}

// Combo — кнопка разобранной (выключенная — пустая).
func (k Key) Combo() Combo {
	c, _ := Parse(string(k))
	return c
}

// Mouse — кнопка мыши (MouseX1, MouseX2, MouseMiddle); 0 — это не мышь.
func (k Key) Mouse() int { return k.Combo().Mouse }

// VK — виртуальный код клавиши; 0 — это не клавиша.
func (k Key) VK() uint32 { return k.Combo().VK }

// Label — подпись кнопки для журнала: «Mouse 4», «Mouse 3», «Ctrl+Q», «F7».
// Страница подписывает сама, на языке программы.
func (k Key) Label() string {
	c, ok := Parse(string(k))
	if !ok {
		return ""
	}
	switch c.Mouse {
	case MouseX1:
		return "Mouse 4"
	case MouseX2:
		return "Mouse 5"
	case MouseMiddle:
		return "Mouse 3"
	}
	var parts []string
	for _, m := range modNames {
		if c.Mods&m.bit != 0 {
			parts = append(parts, strings.ToUpper(m.name[:1])+m.name[1:])
		}
	}
	n := vkName(c.VK)
	if len(n) <= 3 || strings.HasPrefix(n, "f") {
		n = strings.ToUpper(n)
	} else {
		n = strings.ToUpper(n[:1]) + n[1:]
	}
	return strings.Join(append(parts, n), "+")
}

// Сообщения хуков и флаги.
const (
	wmKeyDown     = 0x0100
	wmKeyUp       = 0x0101
	wmSysKeyDown  = 0x0104
	wmSysKeyUp    = 0x0105
	wmLButtonDown = 0x0201
	wmRButtonDown = 0x0204
	wmMButtonDown = 0x0207
	wmXButtonDn   = 0x020B
	// llkhfInjected — нажатие подделано программой (SendInput), не человеком.
	llkhfInjected = 0x10
	llmhfInjected = 0x01
)

func isKeyDown(msg uintptr) bool { return msg == wmKeyDown || msg == wmSysKeyDown }
func isKeyUp(msg uintptr) bool   { return msg == wmKeyUp || msg == wmSysKeyUp }

// mouseButton — какая кнопка нажата в событии мыши (0 — не нажатие
// средней или боковой: левая, правая, движение, отпускание, колесо).
func mouseButton(msg uintptr, mouseData uint32) int {
	switch msg {
	case wmMButtonDown:
		return MouseMiddle
	case wmXButtonDn:
		switch mouseData >> 16 {
		case 1:
			return MouseX1
		case 2:
			return MouseX2
		}
	}
	return 0
}

// MatchMouse — событие мыши msg (wParam хука) с mouseData и flags
// (MSLLHOOKSTRUCT) — нажатие выбранной кнопки. Подделанные нажатия не берём.
func MatchMouse(k Key, msg uintptr, mouseData, flags uint32) bool {
	if msg != wmMButtonDown && msg != wmXButtonDn { // движение и прочее — сразу мимо
		return false
	}
	b := k.Mouse()
	return b != 0 && flags&llmhfInjected == 0 && mouseButton(msg, mouseData) == b
}

// MatchKey — нажатие (не автоповтор — его отсекает Repeat) клавиши msg/vk с
// модификаторами mods — выбранная кнопка. Кнопка с модификаторами — ровно
// эти модификаторы (Ctrl+Q не срабатывает от Ctrl+Shift+Q); голая (F5) —
// при любых, как было до записи нажатием: в бою могут держать Shift.
func MatchKey(k Key, msg uintptr, vk, flags uint32, mods uint8) bool {
	c := k.Combo()
	if c.VK == 0 || !isKeyDown(msg) || vk != c.VK || flags&llkhfInjected != 0 {
		return false
	}
	return c.Mods == 0 || c.Mods == realMods(mods)
}

// Repeat отличает новое нажатие от автоповтора удержанной клавиши: хук
// получает keydown на каждый повтор, отдельного флага у него нет.
//
// Отпускание хук может и не увидеть (хук сняли раньше, Win+L, UAC, окно
// с правами администратора) — тогда клавиша «держалась» бы вечно. Поэтому
// при повторе спрашиваем Held: внутри низкоуровневого хука
// GetAsyncKeyState ещё показывает состояние до этого события — при
// автоповторе клавиша зажата, а после потерянного отпускания — нет.
type Repeat struct {
	down map[uint32]bool
	// Held — клавиша физически зажата (до текущего события); nil — не
	// проверять.
	Held func(vk uint32) bool
}

// Down — клавиша нажата; true — это новое нажатие, false — повтор.
func (r *Repeat) Down(vk uint32) bool {
	if r.down == nil {
		r.down = map[uint32]bool{}
	}
	if r.down[vk] {
		if r.Held != nil && !r.Held(vk) {
			return true // отпускание потерялось — это новое нажатие
		}
		return false
	}
	r.down[vk] = true
	return true
}

// Up — клавиша отпущена.
func (r *Repeat) Up(vk uint32) { delete(r.down, vk) }

// Ошибки записи кнопки (Record).
var (
	ErrCancel      = errors.New("запись кнопки отменена")
	ErrTimeout     = errors.New("кнопку не нажали")
	ErrBusy        = errors.New("кнопку уже записывают")
	ErrUnsupported = errors.New("запись кнопки есть только в Windows")
)

// Verdict — что делать с нажатием при записи кнопки.
type Verdict int

const (
	Ignore  Verdict = iota // не наше (левая/правая мышь, модификатор, отпускание): ждём дальше
	Accept                 // это и есть новая кнопка
	NeedMod                // голая клавиша: нужен Ctrl/Alt/Shift/Win, ждём дальше
	Cancel                 // Esc — отменить запись
)

// DecideMouse — событие мыши при записи. Левая и правая не годятся (ими
// жмут на саму страницу), средняя и боковые — да.
func DecideMouse(msg uintptr, mouseData, flags uint32) (Verdict, Key) {
	if flags&llmhfInjected != 0 {
		return Ignore, ""
	}
	if b := mouseButton(msg, mouseData); b != 0 {
		return Accept, Combo{Mouse: b}.String()
	}
	return Ignore, ""
}

// DecideKey — нажатие клавиши при записи (только keydown; автоповтор
// безвреден — запись кончается на первом решении).
func DecideKey(msg uintptr, vk, flags uint32, mods uint8) (Verdict, Key) {
	if !isKeyDown(msg) || flags&llkhfInjected != 0 || isModifierVK(vk) || vk == 0 || vk > 0xFE {
		return Ignore, ""
	}
	if vk == vkEsc {
		return Cancel, ""
	}
	mods = realMods(mods)
	if mods == 0 && !BareOK(vk) {
		return NeedMod, ""
	}
	return Accept, Combo{VK: vk, Mods: mods}.String()
}
