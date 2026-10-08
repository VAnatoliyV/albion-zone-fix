//go:build darwin || linux

package paddle

import (
	"fmt"
	"runtime"

	"github.com/ebitengine/purego"
)

// LibName — файл ONNX Runtime (на маке — только для проверок).
var LibName = map[string]string{"darwin": "libonnxruntime.dylib", "linux": "libonnxruntime.so"}[runtime.GOOS]

func openSym(path, name string) (uintptr, error) {
	h, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return 0, fmt.Errorf("не загрузилась %s: %w", path, err)
	}
	p, err := purego.Dlsym(h, name)
	if err != nil {
		return 0, fmt.Errorf("%s: нет %s: %w", path, name, err)
	}
	return p, nil
}

// call зовёт функцию C по адресу.
//
//go:uintptrescapes
func call(fn uintptr, args ...uintptr) uintptr {
	r, _, _ := purego.SyscallN(fn, args...)
	return r
}
