//go:build !windows

package oldcopy

import (
	"os"
	"time"
)

// System — вне Windows (разработка на маке) процессов и автозапуска не
// знаем: старых копий не находится.
func System(logf func(string, ...any)) Env {
	return Env{Self: uint32(os.Getpid()), Logf: logf}
}

// WinCloser — вне Windows ничего не закрывает.
type WinCloser struct{}

func (WinCloser) Soft(Proc)                              {}
func (WinCloser) Kill(Proc) error                        { return nil }
func (WinCloser) Wait(ps []Proc, _ time.Duration) []Proc { return nil }

// Retarget — вне Windows задачи нет.
func Retarget(string) error { return nil }
