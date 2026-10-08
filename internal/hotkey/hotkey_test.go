package hotkey

import "testing"

func TestKeys(t *testing.T) {
	if Normalize("") != Default || Normalize("XButton2") != "xbutton2" || Normalize("f13") != Default || Normalize("off") != Off {
		t.Fatal("Normalize")
	}
	if Key("xbutton1").Mouse() != 1 || Key("xbutton2").Mouse() != 2 || Key("f1").Mouse() != 0 {
		t.Fatal("Mouse")
	}
	if Key("f1").VK() != 0x70 || Key("f12").VK() != 0x7B || Key("xbutton1").VK() != 0 || Key("f0").VK() != 0 {
		t.Fatal("VK")
	}
	if len(Choices()) != 15 || Key("xbutton1").Label() != "Mouse 4" || Key("f5").Label() != "F5" {
		t.Fatal(Choices())
	}
}

func TestMatch(t *testing.T) {
	x1 := uint32(1) << 16
	x2 := uint32(2) << 16
	if !MatchMouse("xbutton1", wmXButtonDn, x1, 0) {
		t.Error("кнопка 4")
	}
	if MatchMouse("xbutton1", wmXButtonDn, x2, 0) || MatchMouse("xbutton1", 0x020C, x1, 0) {
		t.Error("кнопка 5 и отпускание — не наше")
	}
	if MatchMouse("xbutton1", wmXButtonDn, x1, llmhfInjected) {
		t.Error("подделанное нажатие не берём")
	}
	if MatchMouse("f5", wmXButtonDn, x1, 0) || MatchMouse(Off, wmXButtonDn, x1, 0) {
		t.Error("выбрана клавиша — мышь не ловим")
	}
	if !MatchKey("f5", wmKeyDown, 0x74, 0) || !MatchKey("f5", wmSysKeyDown, 0x74, 0) {
		t.Error("F5")
	}
	if MatchKey("f5", 0x0101, 0x74, 0) || MatchKey("f5", wmKeyDown, 0x75, 0) || MatchKey("f5", wmKeyDown, 0x74, llkhfInjected) {
		t.Error("отпускание, другая клавиша, подделка")
	}
}
