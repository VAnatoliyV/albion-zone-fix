package paddle

import (
	"errors"
	"fmt"
	"testing"
)

// Причина неудачной загрузки onnxruntime.dll: нет VC++ runtime — только
// когда Windows ответила «модуль не найден» (126) и в System32 нет хотя бы
// одной из нужных библиотек.
func TestMissingVCRuntime(t *testing.T) {
	all := func(string) bool { return true }
	none := func(string) bool { return false }
	noOne := func(n string) bool { return n != "vcruntime140_1.dll" }
	for _, c := range []struct {
		code uintptr
		have func(string) bool
		want bool
	}{
		{errModNotFound, none, true},
		{errModNotFound, noOne, true},  // старый VC++ 2015/2017 без _1
		{errModNotFound, all, false},   // всё есть — не найдено что-то другое
		{errBadExeFormat, none, false}, // не та разрядность — не runtime
		{5, none, false},               // нет доступа
	} {
		if got := MissingVCRuntime(c.code, c.have); got != c.want {
			t.Errorf("код %d: %v, ждали %v", c.code, got, c.want)
		}
	}
	err := fmt.Errorf("не загрузилась onnxruntime.dll: %w", ErrNoVCRuntime)
	if !errors.Is(err, ErrNoVCRuntime) {
		t.Error("ErrNoVCRuntime не узнаётся через обёртку")
	}
}
