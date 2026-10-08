//go:build windows

package paddle

import (
	"fmt"
	"syscall"

	"golang.org/x/sys/windows"
)

// LibName — файл ONNX Runtime в папке ocr.
const LibName = "onnxruntime.dll"

// openSym грузит библиотеку по полному пути. LOAD_WITH_ALTERED_SEARCH_PATH:
// её зависимости ищутся сначала в её же папке, а не рядом с exe.
func openSym(path, name string) (uintptr, error) {
	h, err := windows.LoadLibraryEx(path, 0, windows.LOAD_WITH_ALTERED_SEARCH_PATH)
	if err != nil {
		return 0, fmt.Errorf("не загрузилась %s: %w", path, err)
	}
	p, err := windows.GetProcAddress(h, name)
	if err != nil {
		return 0, fmt.Errorf("%s: нет %s: %w", path, name, err)
	}
	return p, nil
}

// call зовёт функцию C по адресу (на x64 одно соглашение о вызове).
//
//go:uintptrescapes
func call(fn uintptr, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(fn, args...)
	return r
}
