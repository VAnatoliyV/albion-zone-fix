package paddle

import "errors"

// onnxruntime.dll из официального выпуска требует Microsoft Visual C++
// 2015–2022 Redistributable (x64). Программа его не ставит и не возит с
// собой: без него своё распознавание не грузится, работает Windows OCR, а
// человеку — подсказка, что поставить.

// VCRuntimeDLLs — библиотеки VC++ runtime, которые импортирует onnxruntime.dll.
var VCRuntimeDLLs = []string{"vcruntime140.dll", "vcruntime140_1.dll", "msvcp140.dll", "msvcp140_1.dll"}

// VCRedistURL — официальный установщик VC++ Redistributable (x64).
const VCRedistURL = "https://aka.ms/vs/17/release/vc_redist.x64.exe"

// ErrNoVCRuntime — onnxruntime.dll не загрузилась: нет VC++ runtime.
var ErrNoVCRuntime = errors.New("нет Microsoft Visual C++ Redistributable (x64)")

// Коды ошибок LoadLibrary.
const (
	errModNotFound  = 126 // ERROR_MOD_NOT_FOUND: не найдена сама dll или её зависимость
	errBadExeFormat = 193 // ERROR_BAD_EXE_FORMAT
)

// MissingVCRuntime — загрузка не вышла из-за VC++ runtime: Windows ответила
// «модуль не найден» (onnxruntime.dll на месте — значит, не нашлась
// зависимость), и в System32 нет хотя бы одной из VCRuntimeDLLs (have).
func MissingVCRuntime(code uintptr, have func(name string) bool) bool {
	if code != errModNotFound {
		return false
	}
	for _, n := range VCRuntimeDLLs {
		if !have(n) {
			return true
		}
	}
	return false
}
