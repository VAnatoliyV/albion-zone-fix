//go:build windows

package paddle

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

// LibName — файл ONNX Runtime в папке ocr.
const LibName = "onnxruntime.dll"

// loadFlags — где искать зависимости onnxruntime.dll (MSVCP140, VCRUNTIME140…):
// только в её папке (ocr) и в System32 (VC++ Redistributable ставится туда).
// Не в текущей папке и не в PATH: программа работает с правами
// администратора, а туда может подложить dll кто угодно.
const loadFlags = windows.LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR | windows.LOAD_LIBRARY_SEARCH_SYSTEM32

// openSym грузит библиотеку по полному пути (LoadLibraryExW).
func openSym(path, name string) (uintptr, error) {
	if !filepath.IsAbs(path) {
		return 0, fmt.Errorf("путь к %s не полный", path)
	}
	h, err := windows.LoadLibraryEx(path, 0, loadFlags)
	if err != nil {
		var code syscall.Errno
		if errors.As(err, &code) && MissingVCRuntime(uintptr(code), inSystem32) {
			return 0, fmt.Errorf("не загрузилась %s (%v): %w", path, err, ErrNoVCRuntime)
		}
		return 0, fmt.Errorf("не загрузилась %s: %w", path, err)
	}
	p, err := windows.GetProcAddress(h, name)
	if err != nil {
		return 0, fmt.Errorf("%s: нет %s: %w", path, name, err)
	}
	return p, nil
}

// inSystem32 — есть ли библиотека name в System32.
func inSystem32(name string) bool {
	dir, err := windows.GetSystemDirectory()
	if err != nil {
		return true // не узнать — не будем зря советовать
	}
	_, err = os.Stat(filepath.Join(dir, name))
	return err == nil
}

// call зовёт функцию C по адресу (на x64 одно соглашение о вызове).
//
//go:uintptrescapes
func call(fn uintptr, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(fn, args...)
	return r
}
