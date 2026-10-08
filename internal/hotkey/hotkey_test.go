package hotkey

import "testing"

func TestNormalizeOldAndNew(t *testing.T) {
	for in, want := range map[string]Key{
		// старые значения из списка — как были
		"": Default, "xbutton1": "xbutton1", "XButton2": "xbutton2", "f5": "f5", "F12": "f12", "off": Off, "OFF": Off,
		// новые
		"mbutton": "mbutton", "f13": "f13", "F24": "f24", "Ctrl+Q": "ctrl+q", "shift+ctrl+q": "ctrl+shift+q",
		"win+alt+1": "alt+win+1", "ctrl+num+": "ctrl+num+", "alt+vkc0": "alt+vkc0", "ctrl+vk41": "ctrl+a",
		"insert": "insert", "pageup": "pageup", "pagedown": "pagedown", "pause": "pause", "scrolllock": "scrolllock",
		// негодные — по умолчанию
		"q": Default, "home": Default, "end": Default, "space": Default, "mouse9": Default, "f25": Default, "f0": Default,
		"ctrl+": Default, "ctrl+ctrl+q": Default, "hyper+q": Default, "ctrl+esc": Default, "ctrl+shift": Default,
		"ctrl+vka2": Default, "f": Default, "num": Default, "lbutton": Default,
	} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q)=%q, а надо %q", in, got, want)
		}
	}
}

func TestComboRoundTrip(t *testing.T) {
	for _, c := range []Combo{
		{Mouse: MouseX1}, {Mouse: MouseX2}, {Mouse: MouseMiddle},
		{VK: 0x76}, {VK: 0x87}, {VK: 'Q', Mods: ModCtrl}, {VK: '7', Mods: ModCtrl | ModAlt | ModShift | ModWin},
		{VK: 0xC0, Mods: ModAlt}, {VK: 0x6B, Mods: ModCtrl}, {VK: 0x60, Mods: ModCtrl | ModShift}, {VK: 0x20, Mods: ModCtrl},
	} {
		got, ok := Parse(string(c.String()))
		if !ok || got != c {
			t.Errorf("%+v → %q → %+v %v", c, c.String(), got, ok)
		}
	}
	if (Combo{}).String() != Off {
		t.Error("пустая — выключена")
	}
}

func TestLabels(t *testing.T) {
	for k, want := range map[Key]string{
		"xbutton1": "Mouse 4", "xbutton2": "Mouse 5", "mbutton": "Mouse 3", "f7": "F7", "ctrl+q": "Ctrl+Q",
		"ctrl+shift+f2": "Ctrl+Shift+F2", "alt+pageup": "Alt+Pageup", "off": "",
	} {
		if got := k.Label(); got != want {
			t.Errorf("%q: %q, а надо %q", k, got, want)
		}
	}
	if Key("xbutton1").Mouse() != MouseX1 || Key("f1").Mouse() != 0 || Key("f1").VK() != 0x70 || Key("xbutton1").VK() != 0 {
		t.Error("Mouse/VK")
	}
}

func TestMatchMouse(t *testing.T) {
	x1 := uint32(1) << 16
	x2 := uint32(2) << 16
	if !MatchMouse("xbutton1", wmXButtonDn, x1, 0) || !MatchMouse("xbutton2", wmXButtonDn, x2, 0) || !MatchMouse("mbutton", wmMButtonDown, 0, 0) {
		t.Error("наши кнопки")
	}
	if MatchMouse("xbutton1", wmXButtonDn, x2, 0) || MatchMouse("xbutton1", 0x020C, x1, 0) || MatchMouse("mbutton", 0x0208, 0, 0) {
		t.Error("кнопка 5 и отпускание — не наше")
	}
	if MatchMouse("xbutton1", wmXButtonDn, x1, llmhfInjected) {
		t.Error("подделанное нажатие не берём")
	}
	if MatchMouse("f5", wmXButtonDn, x1, 0) || MatchMouse(Off, wmXButtonDn, x1, 0) || MatchMouse("xbutton1", wmLButtonDown, x1, 0) {
		t.Error("выбрана клавиша — мышь не ловим")
	}
}

func TestMatchKey(t *testing.T) {
	if !MatchKey("f5", wmKeyDown, 0x74, 0, 0) || !MatchKey("f5", wmSysKeyDown, 0x74, 0, 0) {
		t.Error("F5")
	}
	if !MatchKey("f5", wmKeyDown, 0x74, 0, ModShift) {
		t.Error("голая F5 — при любых модификаторах, как раньше")
	}
	if MatchKey("f5", wmKeyUp, 0x74, 0, 0) || MatchKey("f5", wmKeyDown, 0x75, 0, 0) || MatchKey("f5", wmKeyDown, 0x74, llkhfInjected, 0) {
		t.Error("отпускание, другая клавиша, подделка")
	}
	if !MatchKey("ctrl+q", wmKeyDown, 'Q', 0, ModCtrl) || !MatchKey("alt+q", wmSysKeyDown, 'Q', 0, ModAlt) {
		t.Error("Ctrl+Q, Alt+Q")
	}
	if MatchKey("ctrl+q", wmKeyDown, 'Q', 0, 0) || MatchKey("ctrl+q", wmKeyDown, 'Q', 0, ModCtrl|ModShift) || MatchKey("ctrl+q", wmKeyDown, 'Q', 0, ModAlt) {
		t.Error("без Ctrl, с лишним Shift, Alt вместо Ctrl — не наше")
	}
	if MatchKey(Off, wmKeyDown, 0x74, 0, 0) || MatchKey("xbutton1", wmKeyDown, 0x74, 0, 0) {
		t.Error("выключена или мышь")
	}
}

func TestRepeat(t *testing.T) {
	var r Repeat
	if !r.Down('Q') || r.Down('Q') || r.Down('Q') {
		t.Fatal("удержание — одно нажатие")
	}
	if !r.Down(0x11) {
		t.Fatal("другая клавиша — своё нажатие")
	}
	r.Up('Q')
	if !r.Down('Q') {
		t.Fatal("отпустил и нажал — новое нажатие")
	}
}

func TestDecide(t *testing.T) {
	type c struct {
		v Verdict
		k Key
	}
	check := func(name string, v Verdict, k Key, want c) {
		t.Helper()
		if v != want.v || k != want.k {
			t.Errorf("%s: %v %q, а надо %v %q", name, v, k, want.v, want.k)
		}
	}
	x := func(n uint32) uint32 { return n << 16 }
	v, k := DecideMouse(wmLButtonDown, 0, 0)
	check("левая", v, k, c{Ignore, ""})
	v, k = DecideMouse(wmRButtonDown, 0, 0)
	check("правая", v, k, c{Ignore, ""})
	v, k = DecideMouse(0x0200, 0, 0)
	check("движение", v, k, c{Ignore, ""})
	v, k = DecideMouse(wmMButtonDown, 0, 0)
	check("средняя", v, k, c{Accept, "mbutton"})
	v, k = DecideMouse(wmXButtonDn, x(1), 0)
	check("X1", v, k, c{Accept, "xbutton1"})
	v, k = DecideMouse(wmXButtonDn, x(2), 0)
	check("X2", v, k, c{Accept, "xbutton2"})
	v, k = DecideMouse(wmXButtonDn, x(1), llmhfInjected)
	check("подделка", v, k, c{Ignore, ""})

	v, k = DecideKey(wmKeyDown, 'Q', 0, 0)
	check("голая буква", v, k, c{NeedMod, ""})
	v, k = DecideKey(wmKeyDown, 0x20, 0, 0)
	check("пробел", v, k, c{NeedMod, ""})
	v, k = DecideKey(wmKeyDown, 0x24, 0, 0)
	check("голый Home", v, k, c{NeedMod, ""})
	v, k = DecideKey(wmKeyDown, 0x76, 0, 0)
	check("F7", v, k, c{Accept, "f7"})
	v, k = DecideKey(wmKeyDown, 0x87, 0, 0)
	check("F24", v, k, c{Accept, "f24"})
	v, k = DecideKey(wmKeyDown, 0x2D, 0, 0)
	check("Insert", v, k, c{Accept, "insert"})
	v, k = DecideKey(wmKeyDown, 0x13, 0, 0)
	check("Pause", v, k, c{Accept, "pause"})
	v, k = DecideKey(wmKeyDown, 'Q', 0, ModCtrl)
	check("Ctrl+Q", v, k, c{Accept, "ctrl+q"})
	v, k = DecideKey(wmSysKeyDown, 'Q', 0, ModAlt)
	check("Alt+Q", v, k, c{Accept, "alt+q"})
	v, k = DecideKey(wmKeyDown, 'Q', 0, ModShift|ModCtrl)
	check("Ctrl+Shift+Q", v, k, c{Accept, "ctrl+shift+q"})
	v, k = DecideKey(wmKeyDown, 0x1B, 0, 0)
	check("Esc", v, k, c{Cancel, ""})
	v, k = DecideKey(wmKeyDown, 0x1B, 0, ModCtrl)
	check("Ctrl+Esc", v, k, c{Cancel, ""})
	v, k = DecideKey(wmKeyDown, 0xA2, 0, ModCtrl)
	check("сам Ctrl", v, k, c{Ignore, ""})
	v, k = DecideKey(wmKeyDown, 0x10, 0, ModShift)
	check("сам Shift", v, k, c{Ignore, ""})
	v, k = DecideKey(wmKeyUp, 'Q', 0, ModCtrl)
	check("отпускание", v, k, c{Ignore, ""})
	v, k = DecideKey(wmKeyDown, 'Q', llkhfInjected, ModCtrl)
	check("подделка", v, k, c{Ignore, ""})
	// Записанное — годная кнопка для настроек.
	for _, key := range []Key{"mbutton", "f7", "insert", "ctrl+q", "alt+q"} {
		if Normalize(string(key)) != key {
			t.Errorf("%q не пережил Normalize", key)
		}
	}
}

// Отпускание потерялось (хук сняли, Win+L, UAC): при следующем нажатии
// клавиша физически не зажата — это новое нажатие, а не повтор.
func TestRepeatLostKeyUp(t *testing.T) {
	held := map[uint32]bool{}
	r := Repeat{Held: func(vk uint32) bool { return held[vk] }}
	if !r.Down(vkEsc) {
		t.Fatal("первое нажатие")
	}
	held[vkEsc] = true // держит — автоповтор
	if r.Down(vkEsc) {
		t.Fatal("автоповтор — не новое нажатие")
	}
	held[vkEsc] = false // отпустил, но хук этого не видел
	if !r.Down(vkEsc) {
		t.Fatal("после потерянного отпускания — новое нажатие")
	}
}

// AltGr (правый Alt с подмешанным Ctrl) — набор символа, а не Ctrl+Alt.
func TestAltGr(t *testing.T) {
	altGr := ModCtrl | ModAlt | ModAltGr
	if v, _ := DecideKey(wmKeyDown, '2', 0, altGr); v != NeedMod {
		t.Errorf("AltGr+2 («@») — нужен модификатор, а не ctrl+alt+2: %v", v)
	}
	if v, k := DecideKey(wmKeyDown, 0x76, 0, altGr); v != Accept || k != "f7" {
		t.Errorf("AltGr+F7 — просто F7: %v %q", v, k)
	}
	if v, _ := DecideKey(wmKeyDown, 'E', 0, altGr|ModShift); v != NeedMod {
		t.Errorf("Shift+AltGr+E — набор символа: %v", v)
	}
	if v, k := DecideKey(wmKeyDown, 'E', 0, ModCtrl|ModAlt); v != Accept || k != "ctrl+alt+e" {
		t.Errorf("настоящий Ctrl+Alt+E: %v %q", v, k)
	}
	if MatchKey("ctrl+alt+e", wmKeyDown, 'E', 0, altGr) || !MatchKey("ctrl+alt+e", wmKeyDown, 'E', 0, ModCtrl|ModAlt) {
		t.Error("ctrl+alt+e не срабатывает от AltGr+E («€»), но срабатывает от Ctrl+Alt+E")
	}
}

func TestCtrlBreak(t *testing.T) {
	if v, k := DecideKey(wmKeyDown, 0x03, 0, ModCtrl); v != Accept || k != "ctrl+break" {
		t.Fatalf("Ctrl+Pause приходит как VK_CANCEL: %v %q", v, k)
	}
	if Key("ctrl+break").Label() != "Ctrl+Break" || Normalize("ctrl+break") != "ctrl+break" || Normalize("break") != Default {
		t.Fatal("подпись и разбор Ctrl+Break")
	}
}

// Shift с буквой — заглавная в чате игры: нужен Ctrl, Alt или Win.
func TestShiftAloneNotEnoughForTyping(t *testing.T) {
	for _, vk := range []uint32{'Q', '7', 0x20, 0xBA, 0xC0, 0xDE, 0x61, 0x6B} {
		if v, k := DecideKey(wmKeyDown, vk, 0, ModShift); v != NeedMod {
			t.Errorf("Shift+%#x: %v %q", vk, v, k)
		}
	}
	for vk, want := range map[uint32]Key{0x76: "shift+f7", 0x2D: "shift+insert", 0x21: "shift+pageup", 0x24: "shift+home", 0x0D: "shift+enter"} {
		if v, k := DecideKey(wmKeyDown, vk, 0, ModShift); v != Accept || k != want {
			t.Errorf("Shift+%#x: %v %q, а надо %q", vk, v, k, want)
		}
	}
	for m, want := range map[uint8]Key{ModCtrl | ModShift: "ctrl+shift+q", ModAlt: "alt+q", ModWin | ModShift: "shift+win+q"} {
		if v, k := DecideKey(wmKeyDown, 'Q', 0, m); v != Accept || k != want {
			t.Errorf("%v: %v %q", m, v, k)
		}
	}
	for _, in := range []string{"shift+q", "shift+1", "shift+space", "shift+vkc0", "shift+num5"} {
		if Normalize(in) != Default {
			t.Errorf("%q из настроек — негодная", in)
		}
	}
	if Normalize("shift+f2") != "shift+f2" || Normalize("ctrl+shift+q") != "ctrl+shift+q" {
		t.Error("Shift+F2 и Ctrl+Shift+Q — годные")
	}
}
