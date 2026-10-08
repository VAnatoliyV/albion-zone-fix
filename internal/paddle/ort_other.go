//go:build !windows && !darwin && !linux

package paddle

import "errors"

// LibName — здесь ONNX Runtime не поддерживается.
const LibName = ""

func openSym(path, name string) (uintptr, error) {
	return 0, errors.New("onnxruntime здесь не поддерживается")
}

func call(fn uintptr, args ...uintptr) uintptr { return 0 }
